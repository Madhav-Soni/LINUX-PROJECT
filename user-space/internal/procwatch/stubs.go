//go:build !linux

package procwatch

import (
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
)

type ProcessInfo struct {
	PID        int       `json:"pid"`
	PPID       int       `json:"ppid"`
	Name       string    `json:"name"`
	State      string    `json:"state"`
	Cmdline    string    `json:"cmdline"`
	SeenAt     time.Time `json:"seen_at"`
	Alive      bool      `json:"alive"`
	TermSignal int       `json:"term_signal,omitempty"`
}

func (p *ProcessInfo) IsZombie() bool { return false }
func (p *ProcessInfo) StateLabel() string { return "unknown" }

type Tracker struct{}
func NewTracker() *Tracker { return &Tracker{} }
func (t *Tracker) Refresh() (appeared, disappeared []int) { return nil, nil }
func (t *Tracker) All() []*ProcessInfo { return nil }
func (t *Tracker) Live() []*ProcessInfo { return nil }
func (t *Tracker) Get(pid int) (*ProcessInfo, bool) { return nil, false }
func (t *Tracker) LiveCount() int { return 0 }
func (t *Tracker) Prune(maxAge time.Duration) {}

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

type Detector struct{}
func NewDetector() *Detector { return &Detector{} }
func (d *Detector) Analyse(tracker *Tracker, appeared, disappeared []int) []LifecycleEvent { return nil }

type NotificationLevel string
const (
	LevelInfo    NotificationLevel = "INFO"
	LevelWarning NotificationLevel = "WARNING"
	LevelAlert   NotificationLevel = "ALERT"
)

type Notification struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Level     NotificationLevel `json:"level"`
	Text      string            `json:"text"`
	PID       int               `json:"pid,omitempty"`
	FaultType events.FaultType  `json:"fault_type,omitempty"`
}

func FormatLifecycle(e LifecycleEvent) Notification { return Notification{} }
func FormatProcessCreated(p *ProcessInfo) Notification { return Notification{} }
func FormatProcessExited(p *ProcessInfo) Notification { return Notification{} }

type LifecycleStore struct{}
func NewLifecycleStore(max int) *LifecycleStore { return &LifecycleStore{} }
func (s *LifecycleStore) Publish(e LifecycleEvent) {}
func (s *LifecycleStore) List(limit int) []LifecycleEvent { return nil }
func (s *LifecycleStore) Subscribe(buffer int) chan LifecycleEvent { return nil }
func (s *LifecycleStore) Unsubscribe(ch chan LifecycleEvent) {}

type NotificationStore struct{}
func NewNotificationStore(max int) *NotificationStore { return &NotificationStore{} }
func (s *NotificationStore) Push(n Notification) {}
func (s *NotificationStore) Recent(limit int) []Notification { return nil }
func (s *NotificationStore) Subscribe(buffer int) chan Notification { return nil }
func (s *NotificationStore) Unsubscribe(ch chan Notification) {}

type Summary struct {
	Timestamp   time.Time      `json:"timestamp"`
	TotalLive   int            `json:"total_live"`
	ZombieCount int            `json:"zombie_count"`
	Processes   []*ProcessInfo `json:"processes"`
}
