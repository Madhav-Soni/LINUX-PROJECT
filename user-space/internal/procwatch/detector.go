//go:build linux

package procwatch

import (
	"fmt"
	"sync"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
)

// LifecycleEvent is a process-lifecycle fault detected by the Detector.
// It is distinct from events.FaultEvent (used by the memory/CPU detector) so
// that the two subsystems can evolve independently.
type LifecycleEvent struct {
	ID         string           `json:"id"`
	Timestamp  time.Time        `json:"timestamp"`
	Type       events.FaultType `json:"type"`
	Severity   events.Severity  `json:"severity"`
	Message    string           `json:"message"`
	PID        int              `json:"pid"`
	PPID       int              `json:"ppid,omitempty"`
	Signal     int              `json:"signal,omitempty"`
	SignalName string           `json:"signal_name,omitempty"`
}

// signalNames maps common Linux signal numbers to their names.
var signalNames = map[int]string{
	1:  "SIGHUP",
	2:  "SIGINT",
	3:  "SIGQUIT",
	4:  "SIGILL",
	6:  "SIGABRT",
	8:  "SIGFPE",
	9:  "SIGKILL",
	11: "SIGSEGV",
	13: "SIGPIPE",
	15: "SIGTERM",
	17: "SIGCHLD",
	19: "SIGSTOP",
}

// sigName resolves a signal number to a printable name.
func sigName(n int) string {
	if s, ok := signalNames[n]; ok {
		return s
	}
	return fmt.Sprintf("SIG%d", n)
}

// Detector analyses each poll cycle's Tracker data and emits LifecycleEvents
// when process-lifecycle faults are detected.
//
// Detection rules implemented:
//  1. Zombie    – process state == "Z"
//  2. Orphan    – PPID flipped to 1 (re-parented to init/systemd)
//  3. ParentNoWait – a parent PID disappeared while live children remain
//  4. ChildKilled  – a process disappeared with a kill-type exit signal
type Detector struct {
	mu          sync.Mutex
	seenZombies map[int]bool // PIDs already reported as zombie
	seenOrphans map[int]bool // PIDs already reported as orphan
	prevPPID    map[int]int  // PPID as of the previous poll
}

// NewDetector returns an initialised Detector.
func NewDetector() *Detector {
	return &Detector{
		seenZombies: make(map[int]bool),
		seenOrphans: make(map[int]bool),
		prevPPID:    make(map[int]int),
	}
}

// Analyse compares the current Tracker state against the previous cycle's
// state and emits zero or more LifecycleEvents describing detected faults.
//
// appeared and disappeared are the PID lists returned by Tracker.Refresh().
func (d *Detector) Analyse(tracker *Tracker, appeared, disappeared []int) []LifecycleEvent {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Build a fast PID -> info lookup for the current cycle.
	all := tracker.All()
	liveByPID := make(map[int]*ProcessInfo, len(all))
	for _, p := range all {
		if p.Alive {
			liveByPID[p.PID] = p
		}
	}

	var evts []LifecycleEvent

	// ── 1. Zombie detection ───────────────────────────────────────────────────
	// A zombie stays in /proc with state "Z" until the parent calls wait().
	for _, p := range liveByPID {
		if !p.IsZombie() || d.seenZombies[p.PID] {
			continue
		}
		d.seenZombies[p.PID] = true
		evts = append(evts, LifecycleEvent{
			ID:        events.NewID(),
			Timestamp: time.Now().UTC(),
			Type:      events.FaultZombie,
			Severity:  events.SeverityWarn,
			Message: fmt.Sprintf(
				"[WARNING] Zombie process detected: %s (PID %d) is in state Z — not reaped by parent (PPID %d)",
				p.Name, p.PID, p.PPID,
			),
			PID:  p.PID,
			PPID: p.PPID,
		})
	}

	// ── 2. Orphan detection ───────────────────────────────────────────────────
	// An orphan is a process whose PPID flipped to 1 — its original parent
	// exited and init (PID 1) adopted it.
	for _, p := range liveByPID {
		if p.PID <= 1 {
			continue
		}
		prev, hasPrev := d.prevPPID[p.PID]
		d.prevPPID[p.PID] = p.PPID

		if hasPrev && prev > 1 && p.PPID == 1 && !d.seenOrphans[p.PID] {
			d.seenOrphans[p.PID] = true
			evts = append(evts, LifecycleEvent{
				ID:        events.NewID(),
				Timestamp: time.Now().UTC(),
				Type:      events.FaultOrphan,
				Severity:  events.SeverityWarn,
				Message: fmt.Sprintf(
					"[WARNING] Orphan process: %s (PID %d) re-parented to init — original parent (PPID %d) exited without wait()",
					p.Name, p.PID, prev,
				),
				PID:  p.PID,
				PPID: prev,
			})
		}
	}

	// ── 3. Parent-exited-without-wait detection ───────────────────────────────
	// When a parent PID disappears from /proc while its live children still
	// report that PPID (briefly, before init adoption completes), we flag the
	// parent exit as a failure to call wait().
	for _, pid := range disappeared {
		parentProc, ok := tracker.Get(pid)
		if !ok {
			continue
		}
		for _, child := range liveByPID {
			// Check current PPID or the previous-cycle PPID for this child.
			prev := d.prevPPID[child.PID]
			if child.PPID == pid || prev == pid {
				evts = append(evts, LifecycleEvent{
					ID:        events.NewID(),
					Timestamp: time.Now().UTC(),
					Type:      events.FaultParentNoWait,
					Severity:  events.SeverityCritical,
					Message: fmt.Sprintf(
						"[WARNING] Parent process %s (PID %d) exited without waiting for child process (PID %d)",
						parentProc.Name, pid, child.PID,
					),
					PID:  child.PID,
					PPID: pid,
				})
			}
		}
	}

	// ── 4. Abrupt child termination ───────────────────────────────────────────
	// When a process disappears with a kill-type signal recorded in its stat,
	// emit a child-killed alert.
	for _, pid := range disappeared {
		proc, ok := tracker.Get(pid)
		if !ok || proc.TermSignal <= 0 {
			continue
		}
		switch proc.TermSignal {
		case 6, 9, 11, 15: // SIGABRT, SIGKILL, SIGSEGV, SIGTERM
			name := sigName(proc.TermSignal)
			evts = append(evts, LifecycleEvent{
				ID:        events.NewID(),
				Timestamp: time.Now().UTC(),
				Type:      events.FaultChildKilled,
				Severity:  events.SeverityCritical,
				Message: fmt.Sprintf(
					"[ALERT] Child process %s (PID %d) terminated unexpectedly via %s",
					proc.Name, pid, name,
				),
				PID:        pid,
				PPID:       proc.PPID,
				Signal:     proc.TermSignal,
				SignalName: name,
			})
		}
	}

	// ── Housekeeping ──────────────────────────────────────────────────────────
	// Remove dead PIDs from tracking maps to prevent unbounded growth.
	for _, pid := range disappeared {
		delete(d.seenZombies, pid)
		delete(d.seenOrphans, pid)
		delete(d.prevPPID, pid)
	}

	return evts
}
