package app

import (
	"sync"

	"github.com/owais/fis/user-space/internal/state"
)

type StatusStore struct {
	mu     sync.RWMutex
	status state.Status
	ok     bool
}

func (s *StatusStore) Set(status state.Status) {
	s.mu.Lock()
	s.status = status
	s.ok = true
	s.mu.Unlock()
}

func (s *StatusStore) Get() (state.Status, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status, s.ok
}
