//go:build linux

package procwatch

import (
	"fmt"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/eventstream"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/logger"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/monitor"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/policy"
)

// NotificationLevel mirrors event severity for the UI notification bar.
type NotificationLevel string

const (
	LevelInfo    NotificationLevel = "INFO"
	LevelWarning NotificationLevel = "WARNING"
	LevelAlert   NotificationLevel = "ALERT"
)

// Notification is a formatted message ready for display.
type Notification struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Level     NotificationLevel `json:"level"`
	Text      string            `json:"text"`
	PID       int               `json:"pid,omitempty"`
	FaultType events.FaultType  `json:"fault_type,omitempty"`
}

// Notifier bridges lifecycle events from the Detector into the event store
// and the logger.
type Notifier struct {
	detector *Detector
	events   *eventstream.Store
	log      *logger.Logger
}

// NewNotifier returns a Notifier.
func NewNotifier(d *Detector, s *eventstream.Store, l *logger.Logger) *Notifier {
	return &Notifier{
		detector: d,
		events:   s,
		log:      l,
	}
}

// Notify runs detection and publishes any found lifecycle faults.
func (n *Notifier) Notify(snapshot monitor.Snapshot, matches map[int]*policy.Target) []events.FaultEvent {
	faults := n.detector.Detect(snapshot, matches)
	for _, f := range faults {
		n.events.Publish(eventstream.NewFaultEvent(f))
		n.log.Info("lifecycle fault", map[string]interface{}{
			"pid":     f.PID,
			"type":    f.Type,
			"message": f.Message,
		})
	}
	return faults
}

// FormatLifecycle converts a LifecycleEvent into a display Notification.
func FormatLifecycle(e events.FaultEvent) Notification {
	return Notification{
		ID:        e.ID,
		Timestamp: e.Timestamp,
		Level:     levelOf(e.Severity),
		Text:      e.Message,
		PID:       e.PID,
		FaultType: e.Type,
	}
}

// FormatProcessCreated emits an [INFO] notification.
func FormatProcessCreated(p *ProcessInfo) Notification {
	return Notification{
		ID:        events.NewID(),
		Timestamp: time.Now().UTC(),
		Level:     LevelInfo,
		Text:      fmt.Sprintf("[INFO] Child Process Created — %s (PID %d, PPID %d)", p.Name, p.PID, p.PPID),
		PID:       p.PID,
		FaultType: "process_created",
	}
}

// FormatProcessExited emits an [INFO] notification.
func FormatProcessExited(p *ProcessInfo) Notification {
	return Notification{
		ID:        events.NewID(),
		Timestamp: time.Now().UTC(),
		Level:     LevelInfo,
		Text:      fmt.Sprintf("[INFO] Process exited — %s (PID %d)", p.Name, p.PID),
		PID:       p.PID,
		FaultType: "process_exited",
	}
}

// levelOf maps event severity to a notification level.
func levelOf(s events.Severity) NotificationLevel {
	switch s {
	case events.SeverityCritical:
		return LevelAlert
	case events.SeverityWarn:
		return LevelWarning
	default:
		return LevelInfo
	}
}
