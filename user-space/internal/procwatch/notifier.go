package procwatch

import (
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/eventstream"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/logger"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/monitor"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/policy"
)

// Notifier wires the procwatch Detector output into the rest of the FIS
// pipeline: eventstream.Store for SSE delivery and logger for structured logs.
//
// Usage in the main poll loop (alongside the existing detector.Detector):
//
//	pwNotifier := procwatch.NewNotifier(procwatch.NewDetector(0), eventStore, log)
//	...
//	// inside runOnce:
//	pwFaults := pwNotifier.Notify(snapshot, matches)
//	for _, fault := range pwFaults {
//	    // optionally route through policy/recovery engine
//	}
type Notifier struct {
	detector   *Detector
	eventStore *eventstream.Store
	log        *logger.Logger
}

// NewNotifier creates a Notifier.
//   - detector   : a procwatch.Detector (owns the Tracker inside)
//   - eventStore : the shared eventstream.Store that drives SSE delivery
//   - log        : structured logger; may be nil (log lines are skipped)
func NewNotifier(
	detector *Detector,
	eventStore *eventstream.Store,
	log *logger.Logger,
) *Notifier {
	return &Notifier{
		detector:   detector,
		eventStore: eventStore,
		log:        log,
	}
}

// Notify runs the lifecycle detector for the current poll snapshot and
// publishes any resulting FaultEvents to the eventstream and logger.
//
// It returns the emitted events so the caller can optionally route them
// through the policy / recovery engine (mirrors the existing flow in main.go).
func (n *Notifier) Notify(
	snapshot monitor.Snapshot,
	matches map[int]*policy.Target,
) []events.FaultEvent {
	faults := n.detector.Detect(snapshot, matches)

	for _, fault := range faults {
		// Publish to SSE stream (picked up by connected browser clients).
		n.eventStore.Publish(eventstream.NewFaultEvent(fault))

		// Structured log entry – same field schema as the existing fault path.
		if n.log != nil {
			n.log.Info("procwatch fault", map[string]interface{}{
				"kind":               "procwatch_event",
				"event_id":           fault.ID,
				"correlation_id":     fault.CorrelationID,
				"target":             fault.Target,
				"pid":                fault.PID,
				"parent_pid":         fault.ParentPID,
				"type":               string(fault.Type),
				"severity":           string(fault.Severity),
				"message":            fault.Message,
				"signal":             fault.Signal,
				"zombie_duration_ns": int64(fault.ZombieDuration),
			})
		}
	}

	return faults
}
