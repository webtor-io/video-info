package services

import (
	"context"
	"encoding/json"
	"fmt"
	iso6391 "github.com/emvi/iso-639-1"
	"net"
	"net/http"
	"regexp"
	"strconv"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/video-info/services/osdb"

	logrusmiddleware "github.com/bakins/logrus-middleware"
)

type Web struct {
	host           string
	port           int
	ln             net.Listener
	searchPool     *SearchPool
	imdbSearchPool *IMDBSearchPool
	subsPool       *SubsPool
	cachePool      *redis.CachePool
	sourceURL      string
}

const (
	WebHostFlag  = "host"
	WebPortFlag  = "port"
	WebSourceURL = "source-url"
)

type Subtitle struct {
	SrcLang string `json:"srclang"`
	Label   string `json:"label"`
	Src     string `json:"src"`
	Format  string `json:"format"`
	ID      string `json:"id"`
}

type Subtitles []Subtitle

func NewWeb(c *cli.Context, sp *SearchPool, isp *IMDBSearchPool, sbp *SubsPool, cp *redis.CachePool) *Web {
	return &Web{
		sourceURL:      c.String(WebSourceURL),
		host:           c.String(WebHostFlag),
		port:           c.Int(WebPortFlag),
		searchPool:     sp,
		imdbSearchPool: isp,
		subsPool:       sbp,
		cachePool:      cp,
	}
}

func RegisterWebFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:  WebHostFlag,
			Usage: "listening host",
			Value: "",
		},
		cli.StringFlag{
			Name:   WebSourceURL,
			Usage:  "source url",
			Value:  "",
			EnvVar: "SOURCE_URL",
		},
		cli.IntFlag{
			Name:  WebPortFlag,
			Usage: "http listening port",
			Value: 8080,
		},
	)
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

func (s *Web) search(ctx context.Context, sourceURL string, imdbID string, purge bool, cache *redis.Cache, logger *log.Entry) ([]osdb.Subtitle, error) {
	var subs []osdb.Subtitle
	var err error
	if imdbID != "" {
		logger.Info("fetching subtitles by IMDB id")
		subs, err = s.imdbSearchPool.Get(ctx, imdbID, cache, purge)
	} else if sourceURL != "" {
		logger.Info("fetching subtitles by hash and file size")
		subs, err = s.searchPool.Get(ctx, sourceURL, cache, purge)
	} else {
		err = errors.Errorf("no data provided to find subtitles")
	}
	return subs, err
}

var (
	re = regexp.MustCompile("(\\d+).([a-z]+)")
)

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Failed to web listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/opensubtitles/", func(w http.ResponseWriter, r *http.Request) {
		values := re.FindStringSubmatch(r.URL.Path)
		if len(values) == 0 {
			w.WriteHeader(400)
			return
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
			logger.WithError(err).WithField("url", r.URL).Error("failed to parse URL")
			w.WriteHeader(400)
			return
		}
		id, err := strconv.Atoi(values[1])
		if err != nil {
			logger.WithError(err).WithField("id", values[1]).Error("failed to parse id")
			w.WriteHeader(400)
			return
		}
		logger = logger.WithField("id", id)
		cache := s.cachePool.Get(getCacheKey(r))
		subs, err := s.search(r.Context(), sourceURL, imdbID, purge, cache, logger)
		if err != nil {
			logger.WithError(err).Error("failed to get subtitles")
			w.WriteHeader(404)
			return
		}

		var sub *osdb.Subtitle
		for _, ss := range subs {
			if ss.Id == strconv.Itoa(id) {
				sub = &ss
				break
			}
		}
		if sub == nil {
			logger.WithField("subs", subs).WithError(err).Error("failed to find subtitle by id")
			w.WriteHeader(404)
			return
		}
		logger.Info("fetching subtitle")

		// src := strings.Replace(sub.SubDownloadLink, "download/", "download/subformat-vtt/", 1)
		su, err := s.subsPool.Get(r.Context(), sub, "webvtt", cache, purge, logger)
		if err != nil {
			logger.WithError(err).Error("failed to get subtitle")
			w.WriteHeader(404)
			return
		}
		// w.Header().Set("Content-Encoding", "gzip")
		// w.Header().Set("Content-Type", "text/vtt;charset=utf-8")
		logger.Info("got subtitle")
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
		subs, err := s.search(r.Context(), sourceURL, imdbID, purge, s.cachePool.Get(getCacheKey(r)), logger)
		if err != nil {
			logger.WithError(err).Error("failed to get subtitles")
			w.WriteHeader(404)
			return
		}
		res := Subtitles{}
		for _, s := range subs {
			label := iso6391.Name(s.Attributes.Language)
			if label == "" {
				label = s.Attributes.Language
			}
			res = append(res, Subtitle{
				SrcLang: s.Attributes.Language,
				Label:   label,
				Src:     fmt.Sprintf("/opensubtitles/%v.%v", s.Id, "vtt"),
				Format:  "vtt",
				ID:      s.Id,
			})
		}
		logger.WithField("subtitles", res).Infof("got subtitles")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	log.Infof("Serving Web at %v", addr)

	logger := log.New()
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
