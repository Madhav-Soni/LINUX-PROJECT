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

// Snapshot is a point-in-time view of the whole system captured from /proc.
type Snapshot struct {
	Timestamp       time.Time
	TotalCPUJiffies uint64
	Processes       []ProcessSnapshot
}

// ProcessSnapshot holds per-process metrics read from /proc/<pid>/*.
type ProcessSnapshot struct {
	PID         int
	PPID        int    // parent PID – read from /proc/<pid>/status
	Name        string
	State       string // single-char Linux state: R S D Z T X
	Cmdline     string
	CPUJiffies  uint64
	MemoryBytes uint64
}

// ReadSnapshot reads a full system snapshot from the /proc filesystem.
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
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	statusData, err := os.ReadFile(statusPath)
	if err != nil {
		return ProcessSnapshot{}, err
	}

	name, state, ppid, memBytes := parseStatus(string(statusData))
	if name == "" {
		return ProcessSnapshot{}, errors.New("missing process name")
	}

	statPath := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return ProcessSnapshot{}, err
	}

	cpuJiffies, err := parseCPUJiffies(string(statData))
	if err != nil {
		return ProcessSnapshot{}, err
	}

	cmdlinePath := filepath.Join("/proc", strconv.Itoa(pid), "cmdline")
	cmdlineData, _ := os.ReadFile(cmdlinePath)
	cmdline := strings.TrimSpace(strings.ReplaceAll(string(cmdlineData), "\x00", " "))

	return ProcessSnapshot{
		PID:         pid,
		PPID:        ppid,
		Name:        name,
		State:       state,
		Cmdline:     cmdline,
		CPUJiffies:  cpuJiffies,
		MemoryBytes: memBytes,
	}, nil
}

// parseStatus extracts Name, State, PPid and VmRSS from /proc/<pid>/status.
func parseStatus(data string) (name, state string, ppid int, memBytes uint64) {
	for _, line := range strings.Split(data, "\n") {
		switch {
		case strings.HasPrefix(line, "Name:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				name = fields[1]
			}
		case strings.HasPrefix(line, "State:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				state = fields[1] // single char
			}
		case strings.HasPrefix(line, "PPid:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				ppid, _ = strconv.Atoi(fields[1])
			}
		case strings.HasPrefix(line, "VmRSS:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					memBytes = kb * 1024
				}
			}
		}
	}
	return name, state, ppid, memBytes
}

func parseCPUJiffies(data string) (uint64, error) {
	end := strings.LastIndex(data, ")")
	if end == -1 || end+2 >= len(data) {
		return 0, errors.New("invalid stat format")
	}
	fields := strings.Fields(data[end+2:])
	if len(fields) < 13 {
		return 0, errors.New("stat fields missing")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("stime: %w", err)
	}
	return utime + stime, nil
}
