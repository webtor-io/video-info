package services

import (
	"context"
	"sync"

	"github.com/webtor-io/video-info/services/redis"
)

type HashPool struct {
	sm sync.Map
}

func NewHashPool() *HashPool {
	return &HashPool{}
}

func (s *HashPool) Get(ctx context.Context, url string, c *redis.Cache, purge bool) (uint64, int64, error) {
	v, loaded := s.sm.LoadOrStore(url, NewHash(url, c))
	if !loaded {
		defer s.sm.Delete(url)
	}
	return v.(*Hash).Get(ctx, purge)
}
