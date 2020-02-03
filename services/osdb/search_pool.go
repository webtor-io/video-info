package osdb

import (
	"sync"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/oz/osdb"
)

type SearchPool struct {
	sm       sync.Map
	cl       *Client
	hashPool *HashPool
}

func NewSearchPool(cl *Client) *SearchPool {
	return &SearchPool{hashPool: NewHashPool(), cl: cl}
}

func (s *SearchPool) Get(url string, c *redis.Cache, purge bool) (osdb.Subtitles, error) {
	v, loaded := s.sm.LoadOrStore(url, NewSearch(url, s.hashPool, s.cl, c))
	if !loaded {
		defer s.sm.Delete(url)
	}
	return v.(*Search).Get(purge)
}
