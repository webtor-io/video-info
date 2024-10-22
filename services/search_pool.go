package services

import (
	"context"
	"github.com/webtor-io/video-info/services/osdb"
	"sync"

	"github.com/webtor-io/video-info/services/redis"
)

type SearchPool struct {
	sm       sync.Map
	cl       *osdb.Client
	hashPool *HashPool
}

func NewSearchPool(cl *osdb.Client) *SearchPool {
	return &SearchPool{
		hashPool: NewHashPool(),
		cl:       cl,
	}
}

func (s *SearchPool) Get(ctx context.Context, url string, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	v, loaded := s.sm.LoadOrStore(url, NewSearch(url, s.hashPool, s.cl, c))
	if !loaded {
		defer s.sm.Delete(url)
	}
	return v.(*Search).Get(ctx, purge)
}
