//go:build linux

package ebpf

import "sync"

const maxStored = 1000

// Store is a thread-safe fixed-size ring buffer for ExecveEvents.
type Store struct {
	mu     sync.RWMutex
	events []ExecveEvent
}

func NewStore() *Store {
	return &Store{
		events: make([]ExecveEvent, 0, maxStored),
	}
}

// Add appends an event, dropping the oldest if at capacity.
func (s *Store) Add(e ExecveEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) >= maxStored {
		s.events = s.events[1:]
	}
	s.events = append(s.events, e)
}

// Recent returns the last n events, newest last.
func (s *Store) Recent(n int) []ExecveEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.events)
	if n > total {
		n = total
	}
	out := make([]ExecveEvent, n)
	copy(out, s.events[total-n:])
	return out
}

// Len returns number of events stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}
