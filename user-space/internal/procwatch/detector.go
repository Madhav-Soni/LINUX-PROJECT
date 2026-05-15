package procwatch

import (
	"fmt"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/monitor"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/policy"
)

// Detector wraps a Tracker and converts lifecycle Deltas into events.FaultEvent
// values for any process that is currently matched by the policy engine.
//
// It is intentionally stateless beyond the embedded Tracker so that the caller
// (detector.Detector or main.go) can decide how to route the produced events.
type Detector struct {
	tracker    *Tracker
	zombieAge  time.Duration
}

// Tracker returns the underlying Tracker so callers can introspect the live
// process registry (e.g. for the /api/v1/procwatch/processes HTTP endpoint).
func (d *Detector) Tracker() *Tracker {
	return d.tracker
}

// NewDetector allocates a Detector.
//
//   - zombieAge: how long a process must be in state Z before a LongZombie
//     alert is raised.  Pass 0 to use the default (3 s).
func NewDetector(zombieAge time.Duration) *Detector {
	return &Detector{
		tracker:   New(),
		zombieAge: zombieAge,
	}
}

// Detect runs one poll cycle.
//
//   - snapshot : current monitor.Snapshot (from monitor.ReadSnapshot)
//   - matches  : map[pid]*policy.Target from engine.MatchProcesses
//
// Returns a (possibly empty) slice of FaultEvents ready to be published.
func (d *Detector) Detect(
	snapshot monitor.Snapshot,
	matches map[int]*policy.Target,
) []events.FaultEvent {

	// Convert monitor.ProcessSnapshot → ProcEntry for the tracker.
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

	// Helper: only emit if the PID (or its parent) is a watched target.
	watchedTarget := func(pid int) string {
		if t := matches[pid]; t != nil {
			return t.Config.Name
		}
		return ""
	}

	// ── Zombie events ─────────────────────────────────────────────────────────
	for _, e := range delta.NewZombies {
		target := watchedTarget(e.PID)
		if target == "" {
			continue
		}
		out = append(out, newLifecycleEvent(
			target, e.PID, e.PPID, 0,
			events.FaultZombie, events.SeverityWarn,
			fmt.Sprintf("process %d (%s) entered zombie state; parent %d has not called wait()", e.PID, e.Name, e.PPID),
			0,
		))
	}
	for _, e := range delta.LongZombies {
		target := watchedTarget(e.PID)
		if target == "" {
			continue
		}
		out = append(out, newLifecycleEvent(
			target, e.PID, e.PPID, 0,
			events.FaultZombie, events.SeverityCritical,
			fmt.Sprintf("process %d (%s) remains zombie for %.1fs; parent %d still has not reaped it",
				e.PID, e.Name, time.Since(e.ZombieSince).Seconds(), e.PPID),
			time.Since(e.ZombieSince),
		))
	}

	// ── Orphan events ─────────────────────────────────────────────────────────
	for _, e := range delta.Orphans {
		target := watchedTarget(e.PID)
		if target == "" {
			continue
		}
		out = append(out, newLifecycleEvent(
			target, e.PID, e.PPID, 0,
			events.FaultOrphan, events.SeverityWarn,
			fmt.Sprintf("process %d (%s) orphaned: re-parented to PID 1 after parent %d exited", e.PID, e.Name, e.PPID),
			0,
		))
	}

	// ── Parent-exit-without-wait events ──────────────────────────────────────
	for _, e := range delta.ParentExits {
		target := watchedTarget(e.PID)
		if target == "" {
			// Also fire if the *parent* was a watched process.
			target = watchedTarget(e.PPID)
		}
		if target == "" {
			continue
		}
		out = append(out, newLifecycleEvent(
			target, e.PID, e.PPID, 0,
			events.FaultParentExit, events.SeverityWarn,
			fmt.Sprintf("parent %d of process %d (%s) exited without reaping the child", e.PPID, e.PID, e.Name),
			0,
		))
	}

	// ── Signal-death events ───────────────────────────────────────────────────
	for _, e := range delta.SignalDeaths {
		target := watchedTarget(e.PID)
		if target == "" {
			continue
		}
		sigName := signalName(e.ExitSignal)
		sev := events.SeverityWarn
		if e.ExitSignal == 9 { // SIGKILL
			sev = events.SeverityCritical
		}
		out = append(out, newLifecycleEvent(
			target, e.PID, e.PPID, e.ExitSignal,
			events.FaultSignalDeath, sev,
			fmt.Sprintf("process %d (%s) killed by %s (signal %d)", e.PID, e.Name, sigName, e.ExitSignal),
			0,
		))
	}

	return out
}

// ── helpers ───────────────────────────────────────────────────────────────────

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

// signalName returns the conventional name for common Linux signal numbers.
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
