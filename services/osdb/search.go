package osdb

import (
	"fmt"
	"sync"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/oz/osdb"

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
	cl       *Client
}

func NewSearch(url string, hp *HashPool, cl *Client, c *redis.Cache) *Search {
	return &Search{url: url, hashPool: hp, cl: cl, cache: c, inited: false}
}

func (s *Search) get(purge bool) (osdb.Subtitles, error) {
	if !purge {
		subtitles, err := s.cache.GetSubtitles()
		if err != nil {
			return nil, errors.Wrap(err, "Failed to get subtitles from cache")
		}
		if subtitles != nil && len(subtitles) > 0 {
			return subtitles, nil
		}
	}
	hash, err := s.hashPool.Get(s.url, s.cache, purge)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get hash")
	}

	cl, err := s.cl.Get()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get OSDB client")
	}
	params := []interface{}{
		cl.Token,
		[]struct {
			Hash string `xmlrpc:"moviehash"`
		}{{
			fmt.Sprintf("%x", hash),
		}},
	}
	subtitles, err := cl.SearchSubtitles(&params)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get subtitles")
	}
	err = s.cache.SetSubtitles(subtitles)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to store subtitles in cache")
	}
	return subtitles, nil
}

func (s *Search) Get(purge bool) (osdb.Subtitles, error) {
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
