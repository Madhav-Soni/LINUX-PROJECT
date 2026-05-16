package procwatch

import (
	"fmt"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/monitor"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/policy"
)

// LifecycleEvent is a subset of events.FaultEvent used for the procwatch UI.
type LifecycleEvent struct {
	ID        string           `json:"id"`
	Timestamp time.Time        `json:"timestamp"`
	Type       events.FaultType `json:"type"`
	Severity   events.Severity  `json:"severity"`
	Message    string           `json:"message"`
	PID        int              `json:"pid"`
	PPID       int              `json:"ppid,omitempty"`
	Signal     int              `json:"signal,omitempty"`
}

// Detector wraps a Tracker and converts lifecycle Deltas into events.FaultEvent
// values for any process that is currently matched by the policy engine.
type Detector struct {
	tracker    *Tracker
	zombieAge  time.Duration
}

// Tracker returns the underlying Tracker.
func (d *Detector) Tracker() *Tracker {
	return d.tracker
}

// NewDetector allocates a Detector.
func NewDetector(zombieAge time.Duration) *Detector {
	return &Detector{
		tracker:   New(),
		zombieAge: zombieAge,
	}
}

// Detect runs one poll cycle.
func (d *Detector) Detect(
	snapshot monitor.Snapshot,
	matches map[int]*policy.Target,
) []events.FaultEvent {

	procs := make([]ProcEntry, 0, len(snapshot.Processes))
	for _, p := range snapshot.Processes {
		procs = append(procs, ProcEntry{
			PID:        p.PID,
			PPID:       p.PPID,
			Name:       p.Name,
			State:      p.State,
			ExitSignal: p.ExitSignal,
		})
	}

	delta := d.tracker.Update(snapshot.Timestamp, procs, d.zombieAge)

	var out []events.FaultEvent

	resolveTarget := func(pid int) string {
		if t := matches[pid]; t != nil {
			return t.Config.Name
		}
		return "untracked"
	}

	for _, e := range delta.NewZombies {
		out = append(out, newLifecycleEvent(
			resolveTarget(e.PID), e.PID, e.PPID, 0,
			events.FaultZombie, events.SeverityWarn,
			fmt.Sprintf("process %d (%s) entered zombie state", e.PID, e.Name),
			0,
		))
	}
	for _, e := range delta.LongZombies {
		out = append(out, newLifecycleEvent(
			resolveTarget(e.PID), e.PID, e.PPID, 0,
			events.FaultZombie, events.SeverityCritical,
			fmt.Sprintf("process %d (%s) remains zombie for %.1fs",
				e.PID, e.Name, time.Since(e.ZombieSince).Seconds()),
			time.Since(e.ZombieSince),
		))
	}

	for _, e := range delta.Orphans {
		out = append(out, newLifecycleEvent(
			resolveTarget(e.PID), e.PID, e.PPID, 0,
			events.FaultOrphan, events.SeverityWarn,
			fmt.Sprintf("process %d (%s) orphaned: re-parented to PID 1", e.PID, e.Name),
			0,
		))
	}

	for _, e := range delta.ParentExits {
		target := resolveTarget(e.PID)
		if target == "untracked" {
			if t := matches[e.PPID]; t != nil {
				target = t.Config.Name
			}
		}
		out = append(out, newLifecycleEvent(
			target, e.PID, e.PPID, 0,
			events.FaultParentExit, events.SeverityWarn,
			fmt.Sprintf("parent %d of process %d (%s) exited without reaping", e.PPID, e.PID, e.Name),
			0,
		))
	}

	for _, e := range delta.SignalDeaths {
		sigName := signalName(e.ExitSignal)
		sev := events.SeverityWarn
		if e.ExitSignal == 9 {
			sev = events.SeverityCritical
		}
		out = append(out, newLifecycleEvent(
			resolveTarget(e.PID), e.PID, e.PPID, e.ExitSignal,
			events.FaultSignalDeath, sev,
			fmt.Sprintf("process %d (%s) killed by %s (signal %d)", e.PID, e.Name, sigName, e.ExitSignal),
			0,
		))
	}

	return out
}

func newLifecycleEvent(
	target string,
	pid, ppid, signal int,
	faultType events.FaultType,
	severity events.Severity,
	message string,
	zombieDur time.Duration,
) events.FaultEvent {
	id := events.NewID()
	return events.FaultEvent{
		ID:             id,
		CorrelationID:  id,
		Timestamp:      time.Now().UTC(),
		Target:         target,
		PID:            pid,
		Type:           faultType,
		Severity:       severity,
		Message:        message,
		ParentPID:      ppid,
		Signal:         signal,
		ZombieDuration: zombieDur,
	}
}

func signalName(sig int) string {
	names := map[int]string{
		1:  "SIGHUP",
		2:  "SIGINT",
		3:  "SIGQUIT",
		6:  "SIGABRT",
		9:  "SIGKILL",
		11: "SIGSEGV",
		13: "SIGPIPE",
		14: "SIGALRM",
		15: "SIGTERM",
	}
	if name, ok := names[sig]; ok {
		return name
	}
	return fmt.Sprintf("SIG(%d)", sig)
}
