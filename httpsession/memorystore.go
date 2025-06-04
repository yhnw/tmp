package httpsession

import (
	"context"
	"sync"
	"time"
)

type memoryStore struct {
	mu sync.RWMutex
	m  map[string]Record
}

func newMemoryStore() *memoryStore {
	return &memoryStore{m: make(map[string]Record)}
}

func (s *memoryStore) Load(_ context.Context, id string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok || time.Now().After(r.IdleDeadline) {
		return nil, nil
	}
	return &r, nil
}

func (s *memoryStore) Save(_ context.Context, r *Record) error {
	if time.Now().After(r.IdleDeadline) {
		return nil
	}
	s.mu.Lock()
	s.m[r.ID] = *r
	s.mu.Unlock()
	return nil
}

func (s *memoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
	return nil
}

func (s *memoryStore) DeleteExpired(_ context.Context) error {
	s.mu.Lock()
	now := time.Now()
	for id, r := range s.m {
		if now.After(r.IdleDeadline) {
			delete(s.m, id)
		}
	}
	s.mu.Unlock()
	return nil
}
