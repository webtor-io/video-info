package services

import (
	"context"
	"fmt"
	"github.com/webtor-io/video-info/services/osdb"
	"sync"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
)

type Search struct {
	url      string
	cache    *redis.Cache
	value    []osdb.Subtitle
	inited   bool
	err      error
	mux      sync.Mutex
	hashPool *HashPool
	cl       *osdb.Client
}

func NewSearch(url string, hp *HashPool, cl *osdb.Client, c *redis.Cache) *Search {
	return &Search{
		url:      url,
		hashPool: hp,
		cl:       cl,
		cache:    c,
		inited:   false,
	}
}

func (s *Search) get(purge bool) ([]osdb.Subtitle, error) {
	if !purge {
		subtitles, err := s.cache.GetSubtitles()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get subtitles from cache")
		}
		if subtitles != nil && len(subtitles) > 0 {
			return subtitles, nil
		}
	}
	hash, _, err := s.hashPool.Get(s.url, s.cache, purge)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get hash")
	}

	ctx := context.Background()
	subtitles, err := s.cl.SearchSubtitlesByHash(ctx, fmt.Sprintf("%x", hash))
	if err != nil {
		return nil, errors.Wrap(err, "failed to get subtitles")
	}
	err = s.cache.SetSubtitles(subtitles)
	if err != nil {
		return nil, errors.Wrap(err, "failed to store subtitles in cache")
	}
	return subtitles, nil
}

func (s *Search) Get(purge bool) ([]osdb.Subtitle, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if purge {
		s.inited = false
	}
	if s.inited {
		return s.value, s.err
	}
	s.value, s.err = s.get(purge)
	s.inited = true
	return s.value, s.err
}
