package services

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/video-info/services/osdb"
)

type Web struct {
	host       string
	port       int
	ln         net.Listener
	searchPool *osdb.SearchPool
	subsPool   *osdb.SubsPool
	cachePool  *redis.CachePool
}

const (
	WEB_HOST_FLAG = "host"
	WEB_PORT_FLAG = "port"
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

func NewWeb(c *cli.Context, sp *osdb.SearchPool, sbp *osdb.SubsPool, cp *redis.CachePool) *Web {
	return &Web{host: c.String(WEB_HOST_FLAG), port: c.Int(WEB_PORT_FLAG), searchPool: sp, subsPool: sbp, cachePool: cp}
}

func RegisterWebFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:  WEB_HOST_FLAG,
		Usage: "listening host",
		Value: "",
	})
	c.Flags = append(c.Flags, cli.IntFlag{
		Name:  WEB_PORT_FLAG,
		Usage: "http listening port",
		Value: 8080,
	})
}

func getSourceURL(r *http.Request) string {
	// return "https://api.webtor.io/08ada5a7a6183aae1e09d831df6748d566095a10/Sintel%2FSintel.mp4?user_id=1ee793ffaf22c1be9eea89fffee93d15&download_id=563a9afab0cb9499367dfe555e2e8f2c&token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhZ2VudCI6Ik1vemlsbGEvNS4wIChNYWNpbnRvc2g7IEludGVsIE1hYyBPUyBYIDEwXzE1XzApIEFwcGxlV2ViS2l0LzUzNy4zNiAoS0hUTUwsIGxpa2UgR2Vja28pIENocm9tZS83OC4wLjM5MDQuMTA4IFNhZmFyaS81MzcuMzYiLCJleHAiOjE1NzQ3Mzc5MTAsInJhdGUiOiIwLjdNIiwiZ3JhY2UiOjMwMCwicHJlc2V0IjoidWx0cmFmYXN0IiwiaWF0IjoxNTc0NzE5OTEwfQ.oMhCPMAaD2T4M5wDZdqAYsT2it-80THimzi7APquGf0"
	return r.Header.Get("X-Source-Url")
}

func getInfoHash(r *http.Request) string {
	return r.Header.Get("X-Info-Hash")
}

func getPath(r *http.Request) string {
	return r.Header.Get("X-Path")
}

func getCacheKey(r *http.Request) string {
	return r.Header.Get("X-Info-Hash") + r.Header.Get("X-Path")
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
		sourceURL := getSourceURL(r)
		purge := r.URL.Query().Get("purge") == "true"
		logger := log.WithFields(log.Fields{
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
		id, err := strconv.Atoi(values[1])
		if err != nil {
			logger.WithError(err).WithField("id", values[1]).Error("Failed to parse id")
			w.WriteHeader(400)
			return
		}
		logger = logger.WithField("id", id)
		logger.Info("Fetching subtitle")
		sub, err := s.subsPool.Get(sourceURL, id, s.cachePool.Get(getCacheKey(r)), purge, logger)
		if err != nil {
			logger.WithError(err).Error("Failed to get subtitle")
			w.WriteHeader(500)
			return
		}
		// w.Header().Set("Content-Encoding", "gzip")
		// w.Header().Set("Content-Type", "text/vtt;charset=utf-8")
		logger.Info("Got subtitle")
		w.Write(sub)
	})
	mux.HandleFunc("/subtitles.json", func(w http.ResponseWriter, r *http.Request) {
		purge := r.URL.Query().Get("purge") == "true"
		sourceURL := getSourceURL(r)
		logger := log.WithFields(log.Fields{
			"infoHash":  getInfoHash(r),
			"path":      getPath(r),
			"sourceURL": sourceURL,
			"purge":     purge,
		})
		logger.Info("Fetching subtitles")
		subs, err := s.searchPool.Get(sourceURL, s.cachePool.Get(getCacheKey(r)), purge)
		if err != nil {
			logger.WithError(err).Error("Failed to get subtitles")
			w.WriteHeader(500)
			return
		}
		res := Subtitles{}
		for _, s := range subs {
			res = append(res, Subtitle{
				SrcLang: s.ISO639,
				Label:   fmt.Sprintf("[OpenSubtitles.org] %s", s.LanguageName),
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
	return http.Serve(ln, mux)
}

func (s *Web) Close() {
	if s.ln != nil {
		s.ln.Close()
	}
}
