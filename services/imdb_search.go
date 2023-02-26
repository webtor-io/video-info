package services

import (
	"context"
	"github.com/webtor-io/video-info/services/osdb"
	"sync"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
)

type IMDBSearch struct {
	imdbID string
	cache  *redis.Cache
	value  []osdb.Subtitle
	inited bool
	err    error
	mux    sync.Mutex
	cl     *osdb.Client
}

func NewIMDBSearch(imdbID string, cl *osdb.Client, c *redis.Cache) *IMDBSearch {
	return &IMDBSearch{imdbID: imdbID, cl: cl, cache: c}
}

func (s *IMDBSearch) get(purge bool) ([]osdb.Subtitle, error) {
	if !purge {
		subtitles, err := s.cache.GetSubtitles()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get subtitles from cache")
		}
		if subtitles != nil && len(subtitles) > 0 {
			return subtitles, nil
		}
	}
	subtitles, err := s.cl.SearchSubtitlesByIMDB(context.Background(), s.imdbID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get subtitles")
	}
	err = s.cache.SetSubtitles(subtitles)
	if err != nil {
		return nil, errors.Wrap(err, "failed to store subtitles in cache")
	}
	return subtitles, nil
}

func (s *IMDBSearch) Get(purge bool) ([]osdb.Subtitle, error) {
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
