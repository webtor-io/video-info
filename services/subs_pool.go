package services

import (
	"context"
	"github.com/pkg/errors"
	"github.com/webtor-io/video-info/services/osdb"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
	"github.com/webtor-io/video-info/services/s3"
)

const (
	SubTTL = 600
)

type SubsPool struct {
	sm     sync.Map
	timers sync.Map
	expire time.Duration
	mux    sync.Mutex
	s3     *s3.S3Storage
	cl     *osdb.Client
}

func NewSubsPool(cl *osdb.Client, s3 *s3.S3Storage) *SubsPool {
	return &SubsPool{
		expire: time.Duration(SubTTL) * time.Second,
		cl:     cl,
		s3:     s3,
	}
}

func (s *SubsPool) Get(ctx context.Context, sub *osdb.Subtitle, format string, c *redis.Cache, purge bool, logger *logrus.Entry) ([]byte, error) {
	if len(sub.Attributes.Files) == 0 {
		return nil, errors.Errorf("no files for subtitle")
	}
	id := sub.Attributes.Files[0].FileId
	key := strconv.Itoa(id) + format
	if purge {
		s.sm.Delete(id)
		s.timers.Delete(id)
	}
	v, _ := s.sm.LoadOrStore(key, NewSub(sub, format, s.cl, c, s.s3, logger))
	t, tLoaded := s.timers.LoadOrStore(key, time.NewTimer(s.expire))
	timer := t.(*time.Timer)
	if !tLoaded {
		go func() {
			<-timer.C
			s.sm.Delete(key)
			s.timers.Delete(key)
		}()
	} else {
		s.mux.Lock()
		timer.Reset(s.expire)
		s.mux.Unlock()
	}
	return v.(*Sub).Get(ctx, purge)
}
