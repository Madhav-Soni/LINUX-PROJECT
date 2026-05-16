//go:build linux

// Package procwatch implements runtime process-lifecycle monitoring by polling
// the Linux /proc filesystem. It detects zombie processes, orphan processes,
// parents that exit without calling wait(), and abrupt child termination via
// signal delivery.
package procwatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessInfo is the runtime record maintained for each observed PID.
type ProcessInfo struct {
	PID        int       `json:"pid"`
	PPID       int       `json:"ppid"`
	Name       string    `json:"name"`
	State      string    `json:"state"`   // R S D Z T X (Linux single-char state)
	Cmdline    string    `json:"cmdline"`
	SeenAt     time.Time `json:"seen_at"`
	Alive      bool      `json:"alive"`
	TermSignal int       `json:"term_signal,omitempty"` // last known exit signal
}

// IsZombie reports whether the process is in the zombie (Z) state.
func (p *ProcessInfo) IsZombie() bool { return p.State == "Z" }

// StateLabel returns a human-readable process state description.
func (p *ProcessInfo) StateLabel() string {
	switch p.State {
	case "R":
		return "running"
	case "S":
		return "sleeping"
	case "D":
		return "disk-wait"
	case "Z":
		return "zombie"
	case "T":
		return "stopped"
	case "X":
		return "dead"
	default:
		return p.State
	}
}

// Tracker maintains a live, thread-safe registry of all processes observed via
// /proc. Call Refresh() on every poll tick to update the registry.
type Tracker struct {
	mu        sync.RWMutex
	processes map[int]*ProcessInfo
}

// NewTracker returns an initialised, empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{processes: make(map[int]*ProcessInfo)}
}

// Refresh scans /proc, updates the internal registry, and returns the PIDs
// that appeared (new) and disappeared (exited) since the previous call.
func (t *Tracker) Refresh() (appeared, disappeared []int) {
	current := readAllProcs()

	t.mu.Lock()
	defer t.mu.Unlock()

	// New processes.
	for pid := range current {
		if _, ok := t.processes[pid]; !ok {
			appeared = append(appeared, pid)
		}
	}

	// Disappeared processes.
	for pid, info := range t.processes {
		if _, ok := current[pid]; !ok && info.Alive {
			info.Alive = false
			disappeared = append(disappeared, pid)
		}
	}

	// Merge current snapshot into registry.
	for pid, info := range current {
		t.processes[pid] = info
	}

	return appeared, disappeared
}

// All returns a copy of every process in the registry (alive and recently dead).
func (t *Tracker) All() []*ProcessInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*ProcessInfo, 0, len(t.processes))
	for _, p := range t.processes {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// Live returns only currently-alive processes.
func (t *Tracker) Live() []*ProcessInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*ProcessInfo, 0, len(t.processes))
	for _, p := range t.processes {
		if p.Alive {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out
}

// Get returns a single process record by PID.
func (t *Tracker) Get(pid int) (*ProcessInfo, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.processes[pid]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

// LiveCount returns the number of currently-alive tracked processes.
func (t *Tracker) LiveCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n := 0
	for _, p := range t.processes {
		if p.Alive {
			n++
		}
	}
	return n
}

// Prune removes dead processes that were last seen more than maxAge ago.
// Call periodically to prevent unbounded growth of the registry.
func (t *Tracker) Prune(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	t.mu.Lock()
	defer t.mu.Unlock()
	for pid, p := range t.processes {
		if !p.Alive && p.SeenAt.Before(cutoff) {
			delete(t.processes, pid)
		}
	}
}

// ── /proc reading helpers ─────────────────────────────────────────────────────

// readAllProcs scans /proc and returns a PID-keyed map of live process info.
func readAllProcs() map[int]*ProcessInfo {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	result := make(map[int]*ProcessInfo, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		info, err := readProcInfo(pid)
		if err != nil {
			continue
		}
		result[pid] = info
	}
	return result
}

// readProcInfo reads /proc/<pid>/status and /proc/<pid>/cmdline to build a
// ProcessInfo. Returns an error if the process vanished mid-read.
func readProcInfo(pid int) (*ProcessInfo, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))

	statusData, err := os.ReadFile(filepath.Join(base, "status"))
	if err != nil {
		return nil, err
	}

	info := &ProcessInfo{
		PID:    pid,
		Alive:  true,
		SeenAt: time.Now().UTC(),
	}

	for _, line := range strings.Split(string(statusData), "\n") {
		switch {
		case strings.HasPrefix(line, "Name:"):
			info.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "State:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				info.State = fields[1] // single character
			}
		case strings.HasPrefix(line, "PPid:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				info.PPID, _ = strconv.Atoi(fields[1])
			}
		}
	}

	if info.Name == "" {
		return nil, fmt.Errorf("no Name field for pid %d", pid)
	}

	// Read cmdline (argv joined by NUL bytes).
	if data, err := os.ReadFile(filepath.Join(base, "cmdline")); err == nil {
		info.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
	}

	// Read exit signal from /proc/<pid>/stat field index 17 (0-based) after
	// the closing ')'. This is valid while the process still exists.
	if statData, err := os.ReadFile(filepath.Join(base, "stat")); err == nil {
		info.TermSignal = parseExitSignal(string(statData))
	}

	return info, nil
}

// parseExitSignal extracts the exit_signal field from /proc/<pid>/stat.
// The layout after the closing ')' is:
//
//	state ppid pgrp session tty_nr tpgid flags minflt cminflt majflt cmajflt
//	utime stime cutime cstime priority nice num_threads itrealvalue starttime
//	vsize rss rlim startcode endcode ...
//
// exit_signal is at index 15 (0-based) in the post-')' fields.
func parseExitSignal(stat string) int {
	end := strings.LastIndex(stat, ")")
	if end < 0 {
		return 0
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 17 {
		return 0
	}
	sig, _ := strconv.Atoi(fields[15])
	return sig
}
