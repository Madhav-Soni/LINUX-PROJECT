package eventstream

import (
	"sync"
	"time"

	"github.com/owais/fis/user-space/internal/events"
)

type Kind string

const (
	KindFault  Kind = "fault"
	KindAction Kind = "action"
)

type ActionEvent struct {
	FaultID       string `json:"fault_id"`
	CorrelationID string `json:"correlation_id"`
	Target        string `json:"target"`
	PID           int    `json:"pid"`
	Action        string `json:"action"`
	Result        string `json:"result"`
	Reason        string `json:"reason,omitempty"`
	Error         string `json:"error,omitempty"`
	NewPID        int    `json:"new_pid,omitempty"`
}

type Event struct {
	ID        string             `json:"id"`
	Timestamp time.Time          `json:"timestamp"`
	Kind      Kind               `json:"kind"`
	Fault     *events.FaultEvent `json:"fault,omitempty"`
	Action    *ActionEvent       `json:"action,omitempty"`
}

type Store struct {
	mu     sync.RWMutex
	max    int
	events []Event
	subs   map[chan Event]struct{}
}

func NewStore(max int) *Store {
	if max <= 0 {
		max = 200
	}
	return &Store{
		max:  max,
		subs: make(map[chan Event]struct{}),
	}
}

func NewFaultEvent(fault events.FaultEvent) Event {
	return Event{
		ID:        fault.ID,
		Timestamp: fault.Timestamp,
		Kind:      KindFault,
		Fault:     &fault,
	}
}

func NewActionEvent(action ActionEvent) Event {
	return Event{
		ID:        events.NewID(),
		Timestamp: time.Now().UTC(),
		Kind:      KindAction,
		Action:    &action,
	}
}

func (s *Store) Publish(event Event) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.events = append(s.events, event)
	if len(s.events) > s.max {
		s.events = s.events[len(s.events)-s.max:]
	}

	subs := make([]chan Event, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Store) List(limit int) []Event {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.events) {
		limit = len(s.events)
	}
	start := len(s.events) - limit
	if start < 0 {
		start = 0
	}
	out := make([]Event, limit)
	copy(out, s.events[start:])
	return out
}

func (s *Store) Subscribe(buffer int) chan Event {
	if s == nil {
		return nil
	}
	if buffer <= 0 {
		buffer = 16
	}
	ch := make(chan Event, buffer)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Store) Unsubscribe(ch chan Event) {
	if s == nil || ch == nil {
		return
	}
	s.mu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.mu.Unlock()
}
