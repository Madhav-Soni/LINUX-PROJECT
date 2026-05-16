//go:build linux

package procwatch

import (
	"fmt"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
)

// NotificationLevel mirrors event severity for the UI notification bar.
type NotificationLevel string

const (
	LevelInfo    NotificationLevel = "INFO"
	LevelWarning NotificationLevel = "WARNING"
	LevelAlert   NotificationLevel = "ALERT"
)

// Notification is a formatted message ready for display in the live
// notification bar of the React dashboard.
type Notification struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Level     NotificationLevel `json:"level"`
	Text      string            `json:"text"`
	PID       int               `json:"pid,omitempty"`
	FaultType events.FaultType  `json:"fault_type,omitempty"`
}

// FormatLifecycle converts a LifecycleEvent into a display Notification.
func FormatLifecycle(e LifecycleEvent) Notification {
	return Notification{
		ID:        e.ID,
		Timestamp: e.Timestamp,
		Level:     levelOf(e.Severity),
		Text:      e.Message,
		PID:       e.PID,
		FaultType: e.Type,
	}
}

// FormatProcessCreated emits an [INFO] notification when a new process appears.
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

// FormatProcessExited emits an [INFO] notification when a process exits cleanly.
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
