package services

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"

	"github.com/webtor-io/video-info/services/redis"

	o "github.com/oz/osdb"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/video-info/services/osdb"

	logrusmiddleware "github.com/bakins/logrus-middleware"
	joonix "github.com/joonix/log"
)

type Web struct {
	host           string
	port           int
	ln             net.Listener
	searchPool     *osdb.SearchPool
	imdbSearchPool *osdb.IMDBSearchPool
	subsPool       *osdb.SubsPool
	cachePool      *redis.CachePool
	sourceURL      string
}

const (
	WEB_HOST_FLAG  = "host"
	WEB_PORT_FLAG  = "port"
	WEB_SOURCE_URL = "source-url"
)

type Subtitle struct {
	SrcLang string `json:"srclang"`
	Label   string `json:"label"`
	Src     string `json:"src"`
	Format  string `json:"format"`
	ID      string `json:"id"`
	Hash    string `json:"hash"`
}

type Subtitles []Subtitle

func NewWeb(c *cli.Context, sp *osdb.SearchPool, isp *osdb.IMDBSearchPool, sbp *osdb.SubsPool, cp *redis.CachePool) *Web {
	return &Web{sourceURL: c.String(WEB_SOURCE_URL), host: c.String(WEB_HOST_FLAG), port: c.Int(WEB_PORT_FLAG), searchPool: sp, imdbSearchPool: isp, subsPool: sbp, cachePool: cp}
}

func RegisterWebFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:  WEB_HOST_FLAG,
		Usage: "listening host",
		Value: "",
	})
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   WEB_SOURCE_URL,
		Usage:  "source url",
		Value:  "",
		EnvVar: "SOURCE_URL",
	})
	c.Flags = append(c.Flags, cli.IntFlag{
		Name:  WEB_PORT_FLAG,
		Usage: "http listening port",
		Value: 8080,
	})
}

func (s *Web) getSourceURL(r *http.Request) string {
	if s.sourceURL != "" {
		return s.sourceURL
	}
	return r.Header.Get("X-Source-Url")
}

func getInfoHash(r *http.Request) string {
	return r.Header.Get("X-Info-Hash")
}

func getPath(r *http.Request) string {
	return r.Header.Get("X-Path")
}

func getCacheKey(r *http.Request) string {
	return r.Header.Get("X-Info-Hash") + r.Header.Get("X-Path") + r.URL.Query().Get("imdb-id")
}

func (s *Web) search(sourceURL string, imdbID string, purge bool, cache *redis.Cache, logger *log.Entry) (o.Subtitles, error) {
	var subs o.Subtitles
	var err error
	if imdbID != "" {
		logger.Info("Fetching subtitles by IMDB id")
		subs, err = s.imdbSearchPool.Get(imdbID, cache, purge)
	} else if sourceURL != "" {
		logger.Info("Fetching subtitles by hash and file size")
		subs, err = s.searchPool.Get(sourceURL, cache, purge)
	} else {
		err = errors.Errorf("No data provided to find subtitles")
	}
	return subs, err
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Failed to web listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	re, _ := regexp.Compile("(\\d+).([a-z]+)")
	mux.HandleFunc("/opensubtitles/", func(w http.ResponseWriter, r *http.Request) {
		values := re.FindStringSubmatch(r.URL.Path)
		if len(values) == 0 {
			w.WriteHeader(400)
		}
		sourceURL := s.getSourceURL(r)
		purge := r.URL.Query().Get("purge") == "true"
		imdbID := r.URL.Query().Get("imdb-id")

		logger := log.WithFields(log.Fields{
			"imdbID":    imdbID,
			"sourceURL": sourceURL,
			"infoHash":  getInfoHash(r),
			"path":      getPath(r),
			"purge":     purge,
		})
		if len(values) == 1 {
			logger.WithError(err).WithField("url", r.URL).Error("Failed to parse URL")
			w.WriteHeader(400)
			return
		}
		id := values[1]
		if err != nil {
			logger.WithError(err).WithField("id", values[1]).Error("Failed to parse id")
			w.WriteHeader(400)
			return
		}
		logger = logger.WithField("id", id)
		cache := s.cachePool.Get(getCacheKey(r))
		subs, err := s.search(sourceURL, imdbID, purge, cache, logger)
		if err != nil {
			logger.WithError(err).Error("Failed to get subtitles")
			w.WriteHeader(500)
			return
		}

		var sub *o.Subtitle
		for _, ss := range subs {
			if ss.IDSubtitleFile == id {
				sub = &ss
				break
			}
		}
		if sub == nil {
			logger.WithField("subs", subs).WithError(err).Error("Failed to find subtitle by id")
			w.WriteHeader(500)
			return
		}
		logger.Info("Fetching subtitle")

		// src := strings.Replace(sub.SubDownloadLink, "download/", "download/subformat-vtt/", 1)
		su, err := s.subsPool.Get(sub.SubDownloadLink, id, cache, purge, logger)
		if err != nil {
			logger.WithError(err).Error("Failed to get subtitle")
			w.WriteHeader(500)
			return
		}
		// w.Header().Set("Content-Encoding", "gzip")
		// w.Header().Set("Content-Type", "text/vtt;charset=utf-8")
		logger.Info("Got subtitle")
		w.Write(su)
	})
	mux.HandleFunc("/subtitles.json", func(w http.ResponseWriter, r *http.Request) {
		purge := r.URL.Query().Get("purge") == "true"
		imdbID := r.URL.Query().Get("imdb-id")
		sourceURL := s.getSourceURL(r)
		logger := log.WithFields(log.Fields{
			"imdbID":    imdbID,
			"infoHash":  getInfoHash(r),
			"path":      getPath(r),
			"sourceURL": sourceURL,
			"purge":     purge,
		})
		subs, err := s.search(sourceURL, imdbID, purge, s.cachePool.Get(getCacheKey(r)), logger)
		if err != nil {
			logger.WithError(err).Error("Failed to get subtitles")
			w.WriteHeader(500)
			return
		}
		res := Subtitles{}
		for _, s := range subs {
			res = append(res, Subtitle{
				SrcLang: s.ISO639,
				Label:   s.LanguageName,
				Src:     fmt.Sprintf("/opensubtitles/%v.%v", s.IDSubtitleFile, s.SubFormat),
				Format:  s.SubFormat,
				ID:      s.IDSubtitleFile,
				Hash:    s.MovieHash,
			})
		}
		logger.WithField("subtitles", res).Infof("Got subtitles")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	log.Infof("Serving Web at %v", addr)

	logger := log.New()
	logger.SetFormatter(joonix.NewFormatter())
	l := logrusmiddleware.Middleware{
		Logger: logger,
	}
	return http.Serve(ln, l.Handler(mux, ""))
}

func (s *Web) Close() {
	if s.ln != nil {
		s.ln.Close()
	}
}
