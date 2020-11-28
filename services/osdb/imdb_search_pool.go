package osdb

import (
	"strings"
	"sync"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/oz/osdb"
)

type IMDBSearchPool struct {
	sm       sync.Map
	cl       *Client
	hashPool *HashPool
}

func NewIMDBSearchPool(cl *Client) *IMDBSearchPool {
	return &IMDBSearchPool{cl: cl}
}

func (s *IMDBSearchPool) Get(imdbID string, c *redis.Cache, purge bool) (osdb.Subtitles, error) {
	imdbID = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(imdbID), "tt"), "0")
	v, loaded := s.sm.LoadOrStore(imdbID, NewIMDBSearch(imdbID, s.cl, c))
	if !loaded {
		defer s.sm.Delete(imdbID)
	}
	return v.(*IMDBSearch).Get(purge)
}
