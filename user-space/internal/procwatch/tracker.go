//go:build linux

package procwatch

import (
	"sync"
	"time"
)

// ProcEntry is a simplified view of a process at a specific point in time.
type ProcEntry struct {
	PID        int
	PPID       int
	Name       string
	State      string
	ExitSignal int
}

// ProcessInfo is the runtime record maintained for each observed PID.
type ProcessInfo struct {
	PID         int       `json:"pid"`
	PPID        int       `json:"ppid"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Cmdline     string    `json:"cmdline"`
	SeenAt      time.Time `json:"seen_at"`
	FirstSeen   time.Time `json:"first_seen"`
	Alive       bool      `json:"alive"`
	ExitSignal  int       `json:"exit_signal,omitempty"`
	ZombieSince time.Time `json:"zombie_since,omitempty"`
}

// IsZombie reports whether the process is in the zombie (Z) state.
func (p *ProcessInfo) IsZombie() bool { return p.State == "Z" }

// Delta describes the differences between the current process state and the previous poll.
type Delta struct {
	Appeared     []*ProcessInfo
	Disappeared  []*ProcessInfo
	NewZombies   []*ProcessInfo
	LongZombies  []*ProcessInfo
	Orphans      []*ProcessInfo
	ParentExits  []*ProcessInfo
	SignalDeaths []*ProcessInfo
}

// Tracker maintains a live, thread-safe registry of all processes observed.
type Tracker struct {
	mu        sync.RWMutex
	processes map[int]*ProcessInfo
}

// New returns an initialised, empty Tracker.
func New() *Tracker {
	return &Tracker{processes: make(map[int]*ProcessInfo)}
}

// NewTracker is an alias for New().
func NewTracker() *Tracker {
	return New()
}

// Update merges a new process snapshot into the tracker.
func (t *Tracker) Update(ts time.Time, entries []ProcEntry, zombieAge time.Duration) Delta {
	t.mu.Lock()
	defer t.mu.Unlock()

	current := make(map[int]ProcEntry, len(entries))
	for _, e := range entries {
		current[e.PID] = e
	}

	delta := Delta{}

	for pid, e := range current {
		if info, ok := t.processes[pid]; !ok || !info.Alive {
			newInfo := &ProcessInfo{
				PID:       e.PID,
				PPID:      e.PPID,
				Name:      e.Name,
				State:     e.State,
				SeenAt:    ts,
				FirstSeen: ts,
				Alive:     true,
			}
			if e.State == "Z" {
				newInfo.ZombieSince = ts
				delta.NewZombies = append(delta.NewZombies, newInfo)
			}
			t.processes[pid] = newInfo
			delta.Appeared = append(delta.Appeared, newInfo)
		}
	}

	for pid, info := range t.processes {
		if !info.Alive {
			continue
		}
		e, stillAlive := current[pid]
		if !stillAlive {
			info.Alive = false
			delta.Disappeared = append(delta.Disappeared, info)
			if info.ExitSignal > 0 && info.ExitSignal != 15 && info.ExitSignal != 2 {
				delta.SignalDeaths = append(delta.SignalDeaths, info)
			}
			continue
		}

		oldState := info.State
		info.State = e.State
		info.SeenAt = ts
		info.ExitSignal = e.ExitSignal

		if oldState != "Z" && e.State == "Z" {
			info.ZombieSince = ts
			delta.NewZombies = append(delta.NewZombies, info)
		} else if e.State == "Z" {
			if !info.ZombieSince.IsZero() && ts.Sub(info.ZombieSince) >= zombieAge {
				delta.LongZombies = append(delta.LongZombies, info)
			}
		} else {
			info.ZombieSince = time.Time{}
		}

		if info.PPID != 1 && e.PPID == 1 {
			delta.Orphans = append(delta.Orphans, info)
		}
		if _, pAlive := current[info.PPID]; !pAlive && info.PPID != 1 && info.PPID != 0 {
			delta.ParentExits = append(delta.ParentExits, info)
		}
		info.PPID = e.PPID
	}
	return delta
}

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
