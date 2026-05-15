// Package procwatch tracks per-process parent-child relationships and lifetime
// state across successive /proc poll cycles.  It is the stateful component
// that feeds zombie, orphan, parent-exit, and signal-death detection.
package procwatch

import (
	"sync"
	"time"
)

// Entry holds the last-known lifecycle state of a single process.
type Entry struct {
	PID  int
	PPID int
	Name string

	// FirstSeen is the wall-clock time at which this PID was first observed.
	FirstSeen time.Time

	// ZombieSince is non-zero when the process was first observed in state "Z".
	// Reset to zero if the process leaves Z (e.g. parent finally reaps it).
	ZombieSince time.Time

	// ExitSignal is the signal extracted from /proc/<pid>/stat on the last poll
	// in which the process was alive.
	ExitSignal int

	// Alive is false once the PID disappears from /proc.
	Alive bool
}

// ProcEntry is a minimal snapshot of a process as seen in the current poll.
// Callers fill this from monitor.ProcessSnapshot.
type ProcEntry struct {
	PID        int
	PPID       int
	Name       string
	State      string // single-char kernel state: R, S, D, Z, T, X …
	ExitSignal int
}

// Tracker maintains a map[PID]Entry and exposes per-cycle deltas.
// All public methods are goroutine-safe.
type Tracker struct {
	mu      sync.Mutex
	entries map[int]*Entry
}

// New allocates a ready-to-use Tracker.
func New() *Tracker {
	return &Tracker{
		entries: make(map[int]*Entry),
	}
}

// Delta describes lifecycle transitions detected during one Update call.
type Delta struct {
	// NewZombies lists PIDs that entered state "Z" this cycle.
	NewZombies []Entry

	// LongZombies lists PIDs that have been in state "Z" for more than the
	// supplied threshold (they already appeared in a previous NewZombies list).
	LongZombies []Entry

	// Orphans lists PIDs whose PPID changed to 1 (or whose parent is no longer
	// alive in the current snapshot) since the last poll.
	Orphans []Entry

	// ParentExits lists child PIDs whose direct parent PID disappeared from
	// /proc this cycle (parent exited without a prior graceful reaping).
	ParentExits []Entry

	// SignalDeaths lists PIDs that disappeared from /proc and had a non-zero
	// ExitSignal recorded during the last cycle they were alive.
	SignalDeaths []Entry

	// Vanished lists all PIDs that were alive last cycle and are gone now,
	// regardless of cause.  Callers may use this for generic crash detection
	// without double-counting the more specific categories above.
	Vanished []Entry
}

// Update ingests the current-cycle process list and returns lifecycle deltas.
//
//   - now         – current wall-clock time (allows unit testing with fixed clocks)
//   - procs       – all processes visible in /proc this cycle
//   - zombieAge   – how long a zombie must persist before it appears in
//     Delta.LongZombies (0 → use default of 3 s)
func (t *Tracker) Update(now time.Time, procs []ProcEntry, zombieAge time.Duration) Delta {
	if zombieAge <= 0 {
		zombieAge = 3 * time.Second
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Index current snapshot by PID.
	currentByPID := make(map[int]ProcEntry, len(procs))
	for _, p := range procs {
		currentByPID[p.PID] = p
	}

	// Build a set of PIDs that are alive this cycle for fast parent-liveness
	// queries.
	alivePIDs := make(map[int]struct{}, len(procs))
	for _, p := range procs {
		alivePIDs[p.PID] = struct{}{}
	}

	var d Delta

	// ── 1. Detect transitions for processes alive in the *previous* cycle ────
	for pid, prev := range t.entries {
		if !prev.Alive {
			continue
		}
		cur, stillAlive := currentByPID[pid]
		if !stillAlive {
			// Process vanished.
			prev.Alive = false
			t.entries[pid] = prev
			d.Vanished = append(d.Vanished, *prev)

			if prev.ExitSignal != 0 {
				d.SignalDeaths = append(d.SignalDeaths, *prev)
			}
			continue
		}

		// Still alive – check PPID change (orphan / parent-exit).
		if cur.PPID != prev.PPID {
			if cur.PPID == 1 {
				// Re-parented to init → classic orphan.
				d.Orphans = append(d.Orphans, *prev)
			} else {
				// PPID changed to something other than 1.  If the old parent is
				// gone, that's a parent-exit-without-wait scenario.
				if _, parentAlive := alivePIDs[prev.PPID]; !parentAlive {
					d.ParentExits = append(d.ParentExits, *prev)
				}
			}
		} else {
			// Same PPID – but if parent is gone now, it's a parent exit.
			if prev.PPID > 1 {
				if _, parentAlive := alivePIDs[prev.PPID]; !parentAlive {
					d.ParentExits = append(d.ParentExits, *prev)
				}
			}
		}

		// Update entry.
		prev.PPID = cur.PPID
		prev.ExitSignal = cur.ExitSignal

		// Zombie state tracking.
		if cur.State == "Z" {
			if prev.ZombieSince.IsZero() {
				// First cycle in Z.
				prev.ZombieSince = now
				d.NewZombies = append(d.NewZombies, *prev)
			} else if now.Sub(prev.ZombieSince) >= zombieAge {
				// Has been Z for long enough to warrant a repeated alert.
				d.LongZombies = append(d.LongZombies, *prev)
			}
		} else {
			// Left Z state (rare but possible if parent finally reaped).
			prev.ZombieSince = time.Time{}
		}

		t.entries[pid] = prev
	}

	// ── 2. Register brand-new PIDs ───────────────────────────────────────────
	for _, cur := range procs {
		if _, known := t.entries[cur.PID]; known {
			continue
		}
		e := &Entry{
			PID:        cur.PID,
			PPID:       cur.PPID,
			Name:       cur.Name,
			FirstSeen:  now,
			ExitSignal: cur.ExitSignal,
			Alive:      true,
		}
		if cur.State == "Z" {
			e.ZombieSince = now
			d.NewZombies = append(d.NewZombies, *e)
		}
		t.entries[cur.PID] = e
	}

	// ── 3. Prune entries that have been dead for a while ─────────────────────
	// Keep dead entries for one full cycle so callers can inspect them; prune
	// on the second visit.
	for pid, e := range t.entries {
		if !e.Alive {
			delete(t.entries, pid)
		}
	}

	return d
}

// Snapshot returns a copy of the current entry map, keyed by PID.
// Useful for debugging and API introspection.
func (t *Tracker) Snapshot() map[int]Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[int]Entry, len(t.entries))
	for pid, e := range t.entries {
		out[pid] = *e
	}
	return out
}

// Reset clears all tracking state.  Use in tests or after a monitored-target
// reconfiguration where all PIDs are expected to be re-learned from scratch.
func (t *Tracker) Reset() {
	t.mu.Lock()
	t.entries = make(map[int]*Entry)
	t.mu.Unlock()
}
