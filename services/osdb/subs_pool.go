package osdb

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
	"github.com/webtor-io/video-info/services/s3"
)

const (
	SUB_TTL = 600
)

type SubsPool struct {
	sm     sync.Map
	timers sync.Map
	expire time.Duration
	mux    sync.Mutex
	s3     *s3.S3Storage
	cl     *Client
}

func NewSubsPool(s3 *s3.S3Storage, cl *Client) *SubsPool {
	return &SubsPool{
		expire: time.Duration(SUB_TTL) * time.Second,
		s3:     s3,
		cl:     cl,
	}
}

func (s *SubsPool) Get(id int, c *redis.Cache, purge bool, logger *logrus.Entry) ([]byte, error) {
	if purge {
		s.sm.Delete(id)
		s.timers.Delete(id)
	}
	v, _ := s.sm.LoadOrStore(id, NewSub(id, s.cl, c, s.s3, logger))
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
