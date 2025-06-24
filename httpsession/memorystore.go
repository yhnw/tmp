package httpsession

import (
	"context"
	"sync"
	"time"
)

type memoryStore[T any] struct {
	mu sync.RWMutex
	m  map[string]Record[T]
}

func newMemoryStore[T any]() *memoryStore[T] {
	return &memoryStore[T]{m: make(map[string]Record[T])}
}

func (s *memoryStore[T]) Load(_ context.Context, id string, ret *Record[T]) (found bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	*ret, found = s.m[id]
	if !found || time.Now().After(ret.IdleDeadline) {
		return false, nil
	}
	return true, nil
}

func (s *memoryStore[T]) Save(_ context.Context, r *Record[T]) error {
	if time.Now().After(r.IdleDeadline) {
		return nil
	}
	s.mu.Lock()
	s.m[r.ID] = *r
	s.mu.Unlock()
	return nil
}

func (s *memoryStore[T]) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
	return nil
}

func (s *memoryStore[T]) DeleteExpired(_ context.Context) error {
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
