package services

import (
	"context"
	"github.com/webtor-io/video-info/services/osdb"
	"sync"

	"github.com/webtor-io/video-info/services/redis"
)

type IMDBSearchPool struct {
	sm       sync.Map
	cl       *osdb.Client
	hashPool *HashPool
}

func NewIMDBSearchPool(cl *osdb.Client) *IMDBSearchPool {
	return &IMDBSearchPool{cl: cl}
}

func (s *IMDBSearchPool) Get(ctx context.Context, q SearchQuery, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	v, loaded := s.sm.LoadOrStore(q.Key(), NewIMDBSearch(q, s.cl, c))
	if !loaded {
		defer s.sm.Delete(q.Key())
	}
	return v.(*IMDBSearch).Get(ctx, purge)
}
