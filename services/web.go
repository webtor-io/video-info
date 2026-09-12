package services

import (
	"context"
	"encoding/json"
	"fmt"
	iso6391 "github.com/emvi/iso-639-1"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/webtor-io/video-info/services/redis"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/video-info/services/osdb"

	logrusmiddleware "github.com/bakins/logrus-middleware"
)

type subtitleSearcher interface {
	ByHash(ctx context.Context, sourceURL string, cache *redis.Cache, purge bool) ([]osdb.Subtitle, error)
	ByIMDB(ctx context.Context, q SearchQuery, cache *redis.Cache, purge bool) ([]osdb.Subtitle, error)
}

type poolSearcher struct {
	hash *SearchPool
	imdb *IMDBSearchPool
}

func (p poolSearcher) ByHash(ctx context.Context, u string, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	return p.hash.Get(ctx, u, c, purge)
}
func (p poolSearcher) ByIMDB(ctx context.Context, q SearchQuery, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	return p.imdb.Get(ctx, c.Key(), q, c, purge)
}

type Web struct {
	host      string
	port      int
	ln        net.Listener
	searcher  subtitleSearcher
	subsPool  *SubsPool
	cachePool *redis.CachePool
	sourceURL string
}

const (
	WebHostFlag  = "host"
	WebPortFlag  = "port"
	WebSourceURL = "source-url"
)

type Subtitle struct {
	SrcLang   string  `json:"srclang"`
	Label     string  `json:"label"`
	Src       string  `json:"src"`
	Format    string  `json:"format"`
	ID        string  `json:"id"`
	Source    string  `json:"source"`
	Release   string  `json:"release,omitempty"`
	Fps       float64 `json:"fps,omitempty"`
	HI        bool    `json:"hi,omitempty"`
	Downloads int     `json:"downloads,omitempty"`
}

type Subtitles []Subtitle

func NewWeb(c *cli.Context, sp *SearchPool, isp *IMDBSearchPool, sbp *SubsPool, cp *redis.CachePool) *Web {
	return &Web{
		sourceURL: c.String(WebSourceURL),
		host:      c.String(WebHostFlag),
		port:      c.Int(WebPortFlag),
		searcher:  poolSearcher{hash: sp, imdb: isp},
		subsPool:  sbp,
		cachePool: cp,
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

// parseSearchQuery reads the search hints. A half-specified episode (only one
// of season/episode) is no hint at all: it cannot be searched by episode, and
// keeping it would only fragment the cache, so both are dropped.
func parseSearchQuery(r *http.Request) SearchQuery {
	q := r.URL.Query()
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("episode"))
	if season <= 0 || episode <= 0 {
		season, episode = 0, 0
	}
	return SearchQuery{ImdbID: q.Get("imdb-id"), Season: season, Episode: episode}
}

// redactURL keeps the location of a source URL and drops its query, which
// carries the access token.
func redactURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	p.RawQuery = ""
	return p.String()
}

// requestLogger describes the request without its secrets.
func (s *Web) requestLogger(r *http.Request, q SearchQuery, sourceURL string, purge bool) *log.Entry {
	return log.WithFields(log.Fields{
		"imdbID":    q.ImdbID,
		"season":    q.Season,
		"episode":   q.Episode,
		"infoHash":  getInfoHash(r),
		"path":      getPath(r),
		"sourceURL": redactURL(sourceURL),
		"purge":     purge,
	})
}

// caches returns the cache of each search leg for this request. See search.
func (s *Web) caches(r *http.Request, q SearchQuery) (hashCache, imdbCache *redis.Cache) {
	infoHash, path := getInfoHash(r), getPath(r)
	return s.cachePool.Get(hashCacheKey(infoHash, path)), s.cachePool.Get(imdbCacheKey(infoHash, path, q))
}

// hashCacheKey is the key of the hash leg: exactly infoHash+path, so the
// moviehash/size entry and the hash search results stay shared across every
// request for the same file, whatever hints the query carries.
func hashCacheKey(infoHash, path string) string {
	return infoHash + path
}

// imdbCacheKey is the key of the IMDb leg: the same file plus the query, since
// results differ per title/season/episode.
func imdbCacheKey(infoHash, path string, q SearchQuery) string {
	return infoHash + path + ":imdb:" + q.Key()
}

// perLangCap is how many tracks per language a listing keeps.
const perLangCap = 3

// search runs the hash leg first and falls back to the IMDb leg. The two legs
// get their own cache: the hash leg keys on the file alone (so its moviehash
// and its results are not fragmented by imdb-id/season/episode hints), the
// IMDb leg keys on file+query. The returned source names the leg that produced
// the list, never a property of the tracks in it.
func (s *Web) search(ctx context.Context, sourceURL string, q SearchQuery, purge bool, hashCache, imdbCache *redis.Cache, logger *log.Entry) ([]osdb.Subtitle, string, error) {
	if sourceURL == "" && !q.Valid() {
		return nil, "", errors.Errorf("no data provided to find subtitles")
	}
	var subs []osdb.Subtitle
	var err error
	if sourceURL != "" {
		logger.Info("fetching subtitles by hash and file size")
		subs, err = s.searcher.ByHash(ctx, sourceURL, hashCache, purge)
		if err != nil {
			logger.WithError(err).Warn("hash search failed")
		}
		if ranked := RankSubtitles(subs, perLangCap); len(ranked) > 0 {
			return ranked, "hash", nil
		}
	}
	if !q.Valid() {
		return nil, "", err
	}
	logger.WithField("episode", q.IsEpisode()).Info("fetching subtitles by IMDB id")
	subs, err = s.searcher.ByIMDB(ctx, q, imdbCache, purge)
	if err != nil {
		return nil, "", err
	}
	return RankSubtitles(subs, perLangCap), "imdb", nil
}

var (
	re = regexp.MustCompile("(\\d+).([a-z]+)")
)

func (s *Web) handleSubtitle(w http.ResponseWriter, r *http.Request) {
	values := re.FindStringSubmatch(r.URL.Path)
	if len(values) == 0 {
		w.WriteHeader(400)
		return
	}
	sourceURL := s.getSourceURL(r)
	purge := r.URL.Query().Get("purge") == "true"
	q := parseSearchQuery(r)
	logger := s.requestLogger(r, q, sourceURL, purge)
	if len(values) == 1 {
		logger.WithField("url", r.URL).Error("failed to parse URL")
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
	hashCache, imdbCache := s.caches(r, q)
	subs, source, err := s.search(r.Context(), sourceURL, q, purge, hashCache, imdbCache, logger)
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
		logger.WithField("count", len(subs)).WithField("source", source).Error("failed to find subtitle by id")
		w.WriteHeader(404)
		return
	}
	logger.Info("fetching subtitle")

	// the subtitle body belongs to the leg that listed it
	cache := hashCache
	if source == "imdb" {
		cache = imdbCache
	}
	su, err := s.subsPool.Get(r.Context(), sub, "webvtt", cache, purge, logger)
	if err != nil {
		logger.WithError(err).Error("failed to get subtitle")
		w.WriteHeader(404)
		return
	}
	logger.Info("got subtitle")
	w.Write(su)
}

func (s *Web) handleSubtitlesJSON(w http.ResponseWriter, r *http.Request) {
	purge := r.URL.Query().Get("purge") == "true"
	q := parseSearchQuery(r)
	sourceURL := s.getSourceURL(r)
	logger := s.requestLogger(r, q, sourceURL, purge)
	hashCache, imdbCache := s.caches(r, q)
	subs, source, err := s.search(r.Context(), sourceURL, q, purge, hashCache, imdbCache, logger)
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
			SrcLang:   s.Attributes.Language,
			Label:     label,
			Src:       fmt.Sprintf("/opensubtitles/%v.%v", s.Id, "vtt"),
			Format:    "vtt",
			ID:        s.Id,
			Source:    source,
			Release:   s.Attributes.Release,
			Fps:       s.Attributes.Fps,
			HI:        s.Attributes.HearingImpaired,
			Downloads: s.Attributes.DownloadCount,
		})
	}
	logger.WithFields(log.Fields{"count": len(res), "source": source}).Infof("got subtitles")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "Failed to web listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/opensubtitles/", s.handleSubtitle)
	mux.HandleFunc("/subtitles.json", s.handleSubtitlesJSON)
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
