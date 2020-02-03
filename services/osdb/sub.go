package osdb

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/oz/osdb"

	"github.com/pkg/errors"
)

type Sub struct {
	url        string
	id         int
	cache      *redis.Cache
	value      []byte
	inited     bool
	err        error
	mux        sync.Mutex
	SearchPool *SearchPool
	logger     *logrus.Entry
}

func NewSub(url string, id int, sp *SearchPool, c *redis.Cache, logger *logrus.Entry) *Sub {
	return &Sub{url: url, id: id, SearchPool: sp, cache: c, inited: false, logger: logger}
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
	subs, err := s.SearchPool.Get(s.url, s.cache, purge)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch subtitles")
	}
	var sub *osdb.Subtitle
	for _, ss := range subs {
		if ss.IDSubtitleFile == fmt.Sprintf("%d", s.id) {
			sub = &ss
			break
		}
	}
	if sub == nil {
		return nil, errors.New("Failed to find subtitle by id")
	}
	// src := strings.Replace(sub.SubDownloadLink, "download/", "download/subformat-vtt/", 1)
	src := sub.SubDownloadLink
	s.logger.WithField("subSrc", src).Info("Fetching subtitle")
	r, err := http.Get(src)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to fetch subtitle url=%v", src)
	}
	if r.StatusCode != http.StatusOK {
		return nil, errors.Errorf("Bad status code=%v url=%v", r.StatusCode, src)
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
