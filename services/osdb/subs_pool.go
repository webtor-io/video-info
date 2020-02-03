package osdb

import (
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
)

type SubsPool struct {
	sm         sync.Map
	searchPool *SearchPool
}

func NewSubsPool(sp *SearchPool) *SubsPool {
	return &SubsPool{searchPool: sp}
}

func (s *SubsPool) Get(url string, id int, c *redis.Cache, purge bool, logger *logrus.Entry) ([]byte, error) {
	v, loaded := s.sm.LoadOrStore(url, NewSub(url, id, s.searchPool, c, logger))
	if !loaded {
		defer s.sm.Delete(url)
	}
	return v.(*Sub).Get(purge)
}
