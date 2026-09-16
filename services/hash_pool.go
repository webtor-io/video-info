package services

import (
	"context"
	"sync"
)

type HashPool struct {
	sm sync.Map
}

func NewHashPool() *HashPool {
	return &HashPool{}
}

func (s *HashPool) Get(ctx context.Context, url string, c hashCache, purge bool) (uint64, int64, error) {
	v, loaded := s.sm.LoadOrStore(url, NewHash(url, c))
	if !loaded {
		defer s.sm.Delete(url)
	}
	return v.(*Hash).Get(ctx, purge)
}
