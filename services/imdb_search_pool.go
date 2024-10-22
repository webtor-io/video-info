package services

import (
	"context"
	"github.com/webtor-io/video-info/services/osdb"
	"strings"
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

func (s *IMDBSearchPool) Get(ctx context.Context, imdbID string, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	imdbID = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(imdbID), "tt"), "0")
	v, loaded := s.sm.LoadOrStore(imdbID, NewIMDBSearch(imdbID, s.cl, c))
	if !loaded {
		defer s.sm.Delete(imdbID)
	}
	return v.(*IMDBSearch).Get(ctx, purge)
}
