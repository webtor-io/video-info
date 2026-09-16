package services

import (
	"context"
	"sync"

	"github.com/webtor-io/video-info/services/osdb"
)

type IMDBSearchPool struct {
	sm sync.Map
	cl *osdb.Client
}

// newIMDBSearch is a var so tests can observe construction.
var newIMDBSearch = NewIMDBSearch

func NewIMDBSearchPool(cl *osdb.Client) *IMDBSearchPool {
	return &IMDBSearchPool{cl: cl}
}

// Get dedups concurrent searches by key, which must identify the file as well
// as the query: the stored IMDBSearch captures the caller's cache, so two
// files of the same title keyed only by the query would share one search bound
// to whichever cache arrived first.
func (s *IMDBSearchPool) Get(ctx context.Context, key string, q SearchQuery, c subtitleCache, purge bool) ([]osdb.Subtitle, error) {
	v, loaded := s.sm.LoadOrStore(key, newIMDBSearch(q, s.cl, c))
	if !loaded {
		defer s.sm.Delete(key)
	}
	return v.(*IMDBSearch).Get(ctx, purge)
}
