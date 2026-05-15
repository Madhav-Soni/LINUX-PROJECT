package monitor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ── snapshot types ────────────────────────────────────────────────────────────

// Snapshot is a point-in-time view of all /proc entries.
type Snapshot struct {
	Timestamp       time.Time
	TotalCPUJiffies uint64
	Processes       []ProcessSnapshot
}

// ProcessSnapshot holds per-process data read from /proc.
// Fields added below the original set are backward-compatible additions.
type ProcessSnapshot struct {
	PID         int
	Name        string
	State       string
	Cmdline     string
	CPUJiffies  uint64
	MemoryBytes uint64

	// ── additions for lifecycle tracking ─────────────────────────────────────

	// PPID is the parent PID as reported in /proc/<pid>/status.
	PPID int

	// ExitSignal is the signal number embedded in the /proc/<pid>/stat field
	// (field index 19 after the closing ')').  Non-zero only when the kernel
	// has already marked the process for delivery of a death signal.
	ExitSignal int

	// ZombieSince records the wall-clock time at which this process was first
	// observed in state "Z".  Zero if the process is not a zombie.
	ZombieSince time.Time
}

// ── public API ────────────────────────────────────────────────────────────────

// ReadSnapshot reads /proc and returns a Snapshot.
// It is safe to call concurrently.
func ReadSnapshot() (Snapshot, error) {
	totalCPU, err := readTotalCPUJiffies()
	if err != nil {
		return Snapshot{}, err
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Snapshot{}, err
	}

	processes := make([]ProcessSnapshot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		proc, err := readProcess(pid)
		if err != nil {
			// Process may have exited between ReadDir and now; skip silently.
			continue
		}
		processes = append(processes, proc)
	}

	return Snapshot{
		Timestamp:       time.Now().UTC(),
		TotalCPUJiffies: totalCPU,
		Processes:       processes,
	}, nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

func readTotalCPUJiffies() (uint64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0, errors.New("/proc/stat is empty")
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 2 || fields[0] != "cpu" {
		return 0, errors.New("/proc/stat cpu line missing")
	}

	var total uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			continue
		}
		total += value
	}
	return total, nil
}

func readProcess(pid int) (ProcessSnapshot, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))

	// ── /proc/<pid>/status ────────────────────────────────────────────────────
	statusData, err := os.ReadFile(filepath.Join(base, "status"))
	if err != nil {
		return ProcessSnapshot{}, err
	}
	name, state, memBytes, ppid := parseStatus(string(statusData))
	if name == "" {
		return ProcessSnapshot{}, errors.New("missing process name")
	}

	// ── /proc/<pid>/stat ──────────────────────────────────────────────────────
	statData, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return ProcessSnapshot{}, err
	}
	cpuJiffies, exitSignal, err := parseStat(string(statData))
	if err != nil {
		return ProcessSnapshot{}, err
	}

	// ── /proc/<pid>/cmdline ───────────────────────────────────────────────────
	cmdlineData, _ := os.ReadFile(filepath.Join(base, "cmdline"))
	cmdline := strings.TrimSpace(strings.ReplaceAll(string(cmdlineData), "\x00", " "))

	return ProcessSnapshot{
		PID:         pid,
		Name:        name,
		State:       state,
		Cmdline:     cmdline,
		CPUJiffies:  cpuJiffies,
		MemoryBytes: memBytes,
		PPID:        ppid,
		ExitSignal:  exitSignal,
	}, nil
}

// parseStatus extracts Name, State, VmRSS (→ bytes), and PPid from
// /proc/<pid>/status.  Returns empty name on failure.
func parseStatus(data string) (name, state string, memBytes uint64, ppid int) {
	for _, line := range strings.Split(data, "\n") {
		switch {
		case strings.HasPrefix(line, "Name:"):
			if f := strings.Fields(line); len(f) >= 2 {
				name = f[1]
			}
		case strings.HasPrefix(line, "State:"):
			if f := strings.Fields(line); len(f) >= 2 {
				state = f[1]
			}
		case strings.HasPrefix(line, "VmRSS:"):
			if f := strings.Fields(line); len(f) >= 2 {
				if kb, err := strconv.ParseUint(f[1], 10, 64); err == nil {
					memBytes = kb * 1024
				}
			}
		case strings.HasPrefix(line, "PPid:"):
			if f := strings.Fields(line); len(f) >= 2 {
				ppid, _ = strconv.Atoi(f[1])
			}
		}
	}
	return
}

// parseStat extracts (utime+stime) and the exit_signal from /proc/<pid>/stat.
//
// The stat format is:
//
//	pid (comm) state ppid pgroup session tty_nr ...
//
// Fields after the closing ')' are indexed from 0.  The mapping used here:
//
//	index  0 → state
//	index  1 → ppid   (redundant; we prefer status)
//	index 11 → utime  (after ')': field[11])
//	index 12 → stime
//	index 17 → exit_signal  (field[17] after ')')
func parseStat(data string) (cpuJiffies uint64, exitSignal int, err error) {
	end := strings.LastIndex(data, ")")
	if end == -1 || end+2 >= len(data) {
		return 0, 0, errors.New("invalid stat format")
	}
	fields := strings.Fields(data[end+2:])
	// minimum required: state(0) ppid(1) pgrp(2) session(3) tty(4) tpgid(5)
	// flags(6) minflt(7) cminflt(8) majflt(9) cmajflt(10) utime(11) stime(12)
	// cutime(13) cstime(14) priority(15) nice(16) num_threads(17→shifted)
	// The Linux kernel proc(5) man page numbers from 1 *after* pid & comm, so
	// our field[0] = state.
	//
	// exit_signal is field index 33 in the raw stat line (1-indexed after pid).
	// After stripping "pid (comm) ", that becomes fields[31].
	if len(fields) < 13 {
		return 0, 0, errors.New("stat fields missing")
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("stime: %w", err)
	}
	cpuJiffies = utime + stime

	// exit_signal: field index 31 after comm-close (0-based).
	// Present in all modern kernels; guard with length check.
	if len(fields) > 31 {
		if sig, err := strconv.Atoi(fields[31]); err == nil {
			exitSignal = sig
		}
	}

	return cpuJiffies, exitSignal, nil
}
