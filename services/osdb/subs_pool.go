package osdb

import (
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
)

type SubsPool struct {
	sm sync.Map
}

func NewSubsPool() *SubsPool {
	return &SubsPool{}
}

func (s *SubsPool) Get(url string, id string, c *redis.Cache, purge bool, logger *logrus.Entry) ([]byte, error) {
	v, loaded := s.sm.LoadOrStore(id, NewSub(url, id, c, logger))
	if !loaded {
		defer s.sm.Delete(id)
	}
	return v.(*Sub).Get(purge)
}
