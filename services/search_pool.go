package services

import (
	"context"
	"sync"

	"github.com/webtor-io/video-info/services/osdb"
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

func (s *SearchPool) Get(ctx context.Context, url string, c searchCache, purge bool) ([]osdb.Subtitle, error) {
	v, loaded := s.sm.LoadOrStore(url, NewSearch(url, s.hashPool, s.cl, c))
	if !loaded {
		defer s.sm.Delete(url)
	}
	return v.(*Search).Get(ctx, purge)
}
