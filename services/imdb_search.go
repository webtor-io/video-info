package services

import (
	"context"
	"fmt"
	"github.com/webtor-io/video-info/services/osdb"
	"sync"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
)

type SearchQuery struct {
	ImdbID  string
	Season  int
	Episode int
}

func (q SearchQuery) IsEpisode() bool { return q.Season > 0 && q.Episode > 0 }

// Valid reports whether the IMDb id is usable as an upstream filter. An id that
// is not valid must never reach the client or the pool key.
func (q SearchQuery) Valid() bool {
	_, ok := osdb.ValidImdbID(q.ImdbID)
	return ok
}

func (q SearchQuery) Key() string {
	return fmt.Sprintf("%s:%d:%d", osdb.NormalizeImdbID(q.ImdbID), q.Season, q.Episode)
}

type IMDBSearch struct {
	q      SearchQuery
	cache  *redis.Cache
	value  []osdb.Subtitle
	inited bool
	err    error
	mux    sync.Mutex
	cl     *osdb.Client
}

func NewIMDBSearch(q SearchQuery, cl *osdb.Client, c *redis.Cache) *IMDBSearch {
	return &IMDBSearch{q: q, cl: cl, cache: c}
}

func (s *IMDBSearch) get(ctx context.Context, purge bool) ([]osdb.Subtitle, error) {
	if !purge {
		subtitles, err := s.cache.GetSubtitles(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get subtitles from cache")
		}
		if subtitles != nil && len(subtitles) > 0 {
			return subtitles, nil
		}
	}
	var subtitles []osdb.Subtitle
	var err error
	if s.q.IsEpisode() {
		subtitles, err = s.cl.SearchSubtitlesByEpisode(ctx, s.q.ImdbID, s.q.Season, s.q.Episode)
	} else {
		subtitles, err = s.cl.SearchSubtitlesByIMDB(ctx, s.q.ImdbID)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get subtitles")
	}
	err = s.cache.SetSubtitles(ctx, subtitles)
	if err != nil {
		return nil, errors.Wrap(err, "failed to store subtitles in cache")
	}
	return subtitles, nil
}

func (s *IMDBSearch) Get(ctx context.Context, purge bool) ([]osdb.Subtitle, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if purge {
		s.inited = false
	}
	if s.inited {
		return s.value, s.err
	}
	s.value, s.err = s.get(ctx, purge)
	s.inited = true
	return s.value, s.err
}
