package osdb

import (
	"io/ioutil"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"
	s "github.com/webtor-io/video-info/services/s3"

	"github.com/pkg/errors"
)

type Sub struct {
	id     int
	cl     *Client
	cache  *redis.Cache
	s3     *s.S3Storage
	value  []byte
	inited bool
	err    error
	mux    sync.Mutex
	logger *logrus.Entry
}

func NewSub(id int, cl *Client, c *redis.Cache, s3 *s.S3Storage, logger *logrus.Entry) *Sub {
	return &Sub{
		id:     id,
		cl:     cl,
		cache:  c,
		logger: logger,
		s3:     s3,
	}
}

func (s *Sub) get(purge bool) ([]byte, error) {
	if !purge {
		subtitle, err := s.cache.GetSubtitle(s.id)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get subtitle from cache")
		}
		if subtitle != nil {
			return subtitle, nil
		}
		if s.s3 != nil {
			subtitle, err := s.s3.GetSub(s.id)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get subtitle from s3")
			}
			if subtitle != nil {
				return subtitle, nil
			}
		}
	}

	cl, err := s.cl.Get()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get OSDB client")
	}
	sfs, err := cl.DownloadSubtitlesByIds([]int{s.id})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get OSDB subtitles")
	}
	if len(sfs) == 0 {
		return nil, errors.Errorf("subtitles empty")
	}
	sf := sfs[0]
	r, err := sf.Reader()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get subtitle reader")
	}
	defer r.Close()

	res, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read subtitle")
	}

	if strings.Contains(string(res), "In order to continue OpenSubtitles.org") {
		return nil, errors.Errorf("failed to get subtitle. Failed to auth (\"In order to continue OpenSubtitles.org subtitles service you need to Log In\")")
	}

	err = s.cache.SetSubtitle(s.id, res)
	if err != nil {
		return nil, errors.Wrap(err, "failed to store subtitle in cache")
	}

	if s.s3 != nil {
		err := s.s3.PutSub(s.id, res)
		if err != nil {
			return nil, errors.Wrap(err, "failed to store subtitle in s3")
		}
	}
	return res, nil
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
