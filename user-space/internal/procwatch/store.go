//go:build linux

package procwatch

import (
	"sync"
	"time"
)

// LifecycleStore is a bounded, fan-out event store for LifecycleEvents.
// It mirrors the design of internal/eventstream.Store but is specialised for
// procwatch so that the two subsystems have no compile-time coupling.
type LifecycleStore struct {
	mu     sync.RWMutex
	max    int
	events []LifecycleEvent
	subs   map[chan LifecycleEvent]struct{}
}

// NewLifecycleStore returns a LifecycleStore that retains at most max events.
func NewLifecycleStore(max int) *LifecycleStore {
	if max <= 0 {
		max = 500
	}
	return &LifecycleStore{
		max:  max,
		subs: make(map[chan LifecycleEvent]struct{}),
	}
}

// Publish appends e to the ring buffer and fans it out to all subscribers.
func (s *LifecycleStore) Publish(e LifecycleEvent) {
	s.mu.Lock()
	s.events = append(s.events, e)
	if len(s.events) > s.max {
		s.events = s.events[len(s.events)-s.max:]
	}
	subs := make([]chan LifecycleEvent, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default: // drop rather than block
		}
	}
}

// List returns the most-recent limit events (oldest first).
func (s *LifecycleStore) List(limit int) []LifecycleEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.events)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]LifecycleEvent, limit)
	copy(out, s.events[n-limit:])
	return out
}

// Subscribe returns a channel that receives future events.
func (s *LifecycleStore) Subscribe(buffer int) chan LifecycleEvent {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan LifecycleEvent, buffer)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the fan-out list and closes it.
func (s *LifecycleStore) Unsubscribe(ch chan LifecycleEvent) {
	if ch == nil {
		return
	}
	s.mu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.mu.Unlock()
}

// NotificationStore is a bounded ring buffer for UI Notifications.
type NotificationStore struct {
	mu    sync.RWMutex
	max   int
	items []Notification
	subs  map[chan Notification]struct{}
}

// NewNotificationStore returns a store that keeps at most max notifications.
func NewNotificationStore(max int) *NotificationStore {
	if max <= 0 {
		max = 200
	}
	return &NotificationStore{
		max:  max,
		subs: make(map[chan Notification]struct{}),
	}
}

// Push appends a notification and fans it out to all subscribers.
func (s *NotificationStore) Push(n Notification) {
	s.mu.Lock()
	s.items = append(s.items, n)
	if len(s.items) > s.max {
		s.items = s.items[len(s.items)-s.max:]
	}
	subs := make([]chan Notification, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- n:
		default:
		}
	}
}

// Recent returns the most-recent limit notifications.
func (s *NotificationStore) Recent(limit int) []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.items)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]Notification, limit)
	copy(out, s.items[n-limit:])
	return out
}

// Subscribe returns a channel that receives future notifications.
func (s *NotificationStore) Subscribe(buffer int) chan Notification {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Notification, buffer)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes ch and closes it.
func (s *NotificationStore) Unsubscribe(ch chan Notification) {
	if ch == nil {
		return
	}
	s.mu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.mu.Unlock()
}

// ── Health summary ────────────────────────────────────────────────────────────

// Summary is returned by /api/v1/procwatch/processes.
type Summary struct {
	Timestamp   time.Time      `json:"timestamp"`
	TotalLive   int            `json:"total_live"`
	ZombieCount int            `json:"zombie_count"`
	Processes   []*ProcessInfo `json:"processes"`
}
