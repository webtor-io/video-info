package osdb

import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
)

type Sub struct {
	url    string
	id     string
	cache  *redis.Cache
	value  []byte
	inited bool
	err    error
	mux    sync.Mutex
	logger *logrus.Entry
}

func NewSub(url string, id string, c *redis.Cache, logger *logrus.Entry) *Sub {
	return &Sub{url: url, id: id, cache: c, inited: false, logger: logger}
}

func (s *Sub) get(purge bool) ([]byte, error) {
	if !purge {
		subtitle, err := s.cache.GetSubtitle(s.id)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to get subtitle from cache")
		}
		if subtitle != nil {
			return subtitle, nil
		}
	}
	s.logger.WithField("subSrc", s.url).Info("Fetching subtitle")
	r, err := http.Get(s.url)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to fetch subtitle url=%v", s.url)
	}
	if r.StatusCode != http.StatusOK {
		return nil, errors.Errorf("Bad status code=%v url=%v", r.StatusCode, s.url)
	}
	body, err := ioutil.ReadAll(r.Body)

	rgz, err := gzip.NewReader(bytes.NewBuffer(body))
	if err != nil {
		return nil, errors.Errorf("Failed to init gzip reader")
	}

	var resBuf bytes.Buffer
	_, err = resBuf.ReadFrom(rgz)
	if err != nil {
		return nil, errors.Errorf("Failed to ungzip")
	}
	res := resBuf.Bytes()

	err = s.cache.SetSubtitle(s.id, res)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to store subtitle in cache")
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
