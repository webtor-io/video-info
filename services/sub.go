package services

import (
	"context"
	"github.com/webtor-io/video-info/services/osdb"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
	s "github.com/webtor-io/video-info/services/s3"

	"github.com/pkg/errors"
)

type Sub struct {
	cl     *osdb.Client
	sub    *osdb.Subtitle
	format string
	cache  *redis.Cache
	s3     *s.S3Storage
	value  []byte
	inited bool
	err    error
	mux    sync.Mutex
	logger *logrus.Entry
}

func NewSub(sub *osdb.Subtitle, format string, cl *osdb.Client, c *redis.Cache, s3 *s.S3Storage, logger *logrus.Entry) *Sub {
	return &Sub{
		sub:    sub,
		format: format,
		cache:  c,
		logger: logger,
		s3:     s3,
		cl:     cl,
	}
}

func (s *Sub) get(purge bool) ([]byte, error) {
	if len(s.sub.Attributes.Files) == 0 {
		return nil, errors.Errorf("no files for subtitle")
	}
	id := s.sub.Attributes.Files[0].FileId
	if !purge {
		subtitle, err := s.cache.GetSubtitle(id, s.format)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get subtitle from cache")
		}
		if subtitle != nil {
			return subtitle, nil
		}
		if s.s3 != nil {
			subtitle, err := s.s3.GetSub(id, s.format)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get subtitle from s3")
			}
			if subtitle != nil {
				return subtitle, nil
			}
		}
	}
	ctx := context.Background()
	d, err := s.cl.DownloadSubtitle(ctx, id, s.format)
	if err != nil {
		return nil, errors.Wrap(err, "failed to download subtitle")
	}
	err = s.cache.SetSubtitle(id, s.format, d)
	if err != nil {
		return nil, errors.Wrap(err, "failed to store subtitle in cache")
	}

	if s.s3 != nil {
		err := s.s3.PutSub(id, s.format, d)
		if err != nil {
			return nil, errors.Wrap(err, "failed to store subtitle in s3")
		}
	}
	return d, nil
}

func (s *Sub) Get(purge bool) ([]byte, error) {
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
