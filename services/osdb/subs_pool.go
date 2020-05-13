package osdb

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
)

const (
	SUB_TTL = 600
)

type SubsPool struct {
	sm     sync.Map
	timers sync.Map
	expire time.Duration
	mux    sync.Mutex
}

func NewSubsPool() *SubsPool {
	return &SubsPool{expire: time.Duration(SUB_TTL) * time.Second}
}

func (s *SubsPool) Get(url string, id string, c *redis.Cache, purge bool, logger *logrus.Entry) ([]byte, error) {
	if purge {
		s.sm.Delete(id)
		s.timers.Delete(id)
	}
	v, _ := s.sm.LoadOrStore(id, NewSub(url, id, c, logger))
	t, tLoaded := s.timers.LoadOrStore(id, time.NewTimer(s.expire))
	timer := t.(*time.Timer)
	if !tLoaded {
		go func() {
			<-timer.C
			s.sm.Delete(id)
			s.timers.Delete(id)
		}()
	} else {
		s.mux.Lock()
		timer.Reset(s.expire)
		s.mux.Unlock()
	}
	return v.(*Sub).Get(purge)
}
