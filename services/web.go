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

// subtitleFetcher reads the body of one track. *SubsPool is the production
// implementation.
type subtitleFetcher interface {
	Get(ctx context.Context, sub *osdb.Subtitle, format string, c *redis.Cache, purge bool, logger *log.Entry) ([]byte, error)
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
	subsPool  subtitleFetcher
	cachePool *redis.CachePool
	sourceURL string
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
	// Source says how confident the sync is for this one track: "hash" when
	// OpenSubtitles matched this exact file, "imdb" when the track only belongs
	// to the same title. It is a property of the track, not of the leg that
	// found it — see trackSource.
	Source string `json:"source"`
	// MoviehashMatch is the raw flag Source is derived from, carried so a
	// consumer can tell the two apart without reading this comment.
	MoviehashMatch bool    `json:"moviehash_match"`
	Release        string  `json:"release,omitempty"`
	Fps            float64 `json:"fps,omitempty"`
	HI             bool    `json:"hi,omitempty"`
	Downloads      int     `json:"downloads,omitempty"`
}

// trackSource reports how the track was matched to the file. A moviehash search
// returns the tracks of the *movie*, not only the tracks that matched the hash,
// and only some of them carry moviehash_match. Reporting the leg would label a
// track that may well drift against this release as "in sync with this exact
// file", which is what web-ui reads it as when it puts the track at the top of
// its ladder.
func trackSource(s osdb.Subtitle) string {
	if s.Attributes.MoviehashMatch {
		return "hash"
	}
	return "imdb"
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

// retryAfterSeconds is how long an unfinished listing asks the client to wait.
const retryAfterSeconds = "5"

// reasonNoQuery is the one failure that is a genuine 404: the request named
// neither a file nor a title, so there is no resource to talk about.
const reasonNoQuery = "no_query"

// reasonClientGone is a cancelled request context: the viewer navigated away
// before the seeder answered. Nothing is wrong with the file.
const reasonClientGone = "client_gone"

// failureReason names why a search leg failed, so the log can split the empty
// listings by cause instead of lumping them into one bucket. leg is "hash" or
// "imdb"; the result is e.g. "hash_timeout", "imdb_error", "client_gone".
func failureReason(leg string, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return reasonClientGone
	}
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return leg + "_timeout"
	}
	return leg + "_error"
}

// search runs the hash leg first and falls back to the IMDb leg. The two legs
// get their own cache: the hash leg keys on the file alone (so its moviehash
// and its results are not fragmented by imdb-id/season/episode hints), the
// IMDb leg keys on file+query. The returned source names the leg that produced
// the list and is used for logging only; what the client is told about a track
// is per-track — see trackSource.
//
// The third return separates "not ready" from "nothing here": it is empty when
// both legs answered, and names the failure when one of them did not. Only a
// request that carries nothing to search by is an error, and only that is a
// 404 — a seeder that could not serve the head and tail bytes in time is not.
func (s *Web) search(ctx context.Context, sourceURL string, q SearchQuery, purge bool, hashCache, imdbCache *redis.Cache, logger *log.Entry) ([]osdb.Subtitle, string, string, error) {
	if sourceURL == "" && !q.Valid() {
		return nil, "", reasonNoQuery, errors.Errorf("no data provided to find subtitles")
	}
	var reason string
	if sourceURL != "" {
		logger.Info("fetching subtitles by hash and file size")
		subs, err := s.searcher.ByHash(ctx, sourceURL, hashCache, purge)
		if err != nil {
			reason = failureReason("hash", err)
			logger.WithError(err).WithField("reason", reason).Warn("hash search failed")
		}
		if ranked := RankSubtitles(subs, perLangCap); len(ranked) > 0 {
			return ranked, "hash", "", nil
		}
	}
	if !q.Valid() {
		return nil, "", reason, nil
	}
	logger.WithField("episode", q.IsEpisode()).Info("fetching subtitles by IMDB id")
	subs, err := s.searcher.ByIMDB(ctx, q, imdbCache, purge)
	if err != nil {
		reason = failureReason("imdb", err)
		logger.WithError(err).WithField("reason", reason).Warn("imdb search failed")
		return nil, "", reason, nil
	}
	ranked := RankSubtitles(subs, perLangCap)
	if len(ranked) == 0 {
		// The IMDb leg has nothing either. If the hash leg failed, the file may
		// still have tracks we simply could not read yet, so keep its reason.
		return nil, "imdb", reason, nil
	}
	return ranked, "imdb", "", nil
}

var (
	re = regexp.MustCompile("(\\d+).([a-z]+)")
)

// trackByID finds one track among a leg's candidates.
func trackByID(subs []osdb.Subtitle, id string) *osdb.Subtitle {
	for i := range subs {
		if subs[i].Id == id {
			return &subs[i]
		}
	}
	return nil
}

// findTrack resolves a track id against the union of both legs and reports the
// cache the track's body belongs to (bodies are keyed per leg).
//
// The id cannot be looked up in a fresh listing, because the listing is not
// stable: on a cold torrent the hash leg fails, the viewer gets an imdb listing,
// and a second later the hash leg succeeds — a re-run search would then return
// the hash list, which does not contain the id the viewer just clicked. An
// expiring 24h cache entry flips it the other way. So a miss in the first leg
// is not a 404 until the other leg has been asked as well.
//
// The lookup runs over the full candidate set of each leg, not the ranked and
// capped listing: ranking answers "what do we show", not "does this id exist".
// The per-track filter stays, so the id space is exactly the set of tracks that
// could ever have been listed.
func (s *Web) findTrack(ctx context.Context, id string, sourceURL string, q SearchQuery, purge bool, hashCache, imdbCache *redis.Cache, logger *log.Entry) (*osdb.Subtitle, *redis.Cache, string) {
	var reason string
	if sourceURL != "" {
		subs, err := s.searcher.ByHash(ctx, sourceURL, hashCache, purge)
		if err != nil {
			reason = failureReason("hash", err)
			logger.WithError(err).WithField("reason", reason).Warn("hash search failed")
		}
		if sub := trackByID(RankSubtitles(subs, 0), id); sub != nil {
			return sub, hashCache, ""
		}
	}
	if q.Valid() {
		subs, err := s.searcher.ByIMDB(ctx, q, imdbCache, purge)
		if err != nil {
			reason = failureReason("imdb", err)
			logger.WithError(err).WithField("reason", reason).Warn("imdb search failed")
		}
		if sub := trackByID(RankSubtitles(subs, 0), id); sub != nil {
			return sub, imdbCache, ""
		}
	}
	return nil, nil, reason
}

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
	if sourceURL == "" && !q.Valid() {
		logger.WithField("reason", reasonNoQuery).Error("failed to get subtitles")
		w.WriteHeader(404)
		return
	}
	hashCache, imdbCache := s.caches(r, q)
	sub, cache, reason := s.findTrack(r.Context(), strconv.Itoa(id), sourceURL, q, purge, hashCache, imdbCache, logger)
	if sub == nil {
		if reason != "" {
			// A leg failed, so the track may well exist and simply could not be
			// read yet — the same distinction /subtitles.json makes. 404 is
			// final to the browser and to the CDN; 503 is not. The header is
			// exposed because the reader here is the player, not web-ui, and
			// the proxy does not add to Expose-Headers on its own.
			logger.WithField("reason", reason).Warn("subtitle not ready")
			w.Header().Set("Retry-After", retryAfterSeconds)
			w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		logger.Error("failed to find subtitle by id")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	logger.Info("fetching subtitle")

	su, err := s.subsPool.Get(r.Context(), sub, "webvtt", cache, purge, logger)
	if err != nil {
		logger.WithError(err).Error("failed to get subtitle")
		w.WriteHeader(404)
		return
	}
	logger.Info("got subtitle")
	_, _ = w.Write(su)
}

func (s *Web) handleSubtitlesJSON(w http.ResponseWriter, r *http.Request) {
	purge := r.URL.Query().Get("purge") == "true"
	q := parseSearchQuery(r)
	sourceURL := s.getSourceURL(r)
	logger := s.requestLogger(r, q, sourceURL, purge)
	hashCache, imdbCache := s.caches(r, q)
	subs, source, reason, err := s.search(r.Context(), sourceURL, q, purge, hashCache, imdbCache, logger)
	if err != nil {
		logger.WithError(err).WithField("reason", reason).Error("failed to get subtitles")
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
			SrcLang:        s.Attributes.Language,
			Label:          label,
			Src:            fmt.Sprintf("/opensubtitles/%v.%v", s.Id, "vtt"),
			Format:         "vtt",
			ID:             s.Id,
			Source:         trackSource(s),
			MoviehashMatch: s.Attributes.MoviehashMatch,
			Release:        s.Attributes.Release,
			Fps:            s.Attributes.Fps,
			HI:             s.Attributes.HearingImpaired,
			Downloads:      s.Attributes.DownloadCount,
		})
	}
	if len(res) == 0 && reason != "" {
		// A leg failed, so this is "not ready yet", not "no such thing". 404 is
		// read as the latter by the browser, by the CDN and by web-ui, which
		// parses the body without looking at the status at all; answer with an
		// empty list of the usual shape and say when to come back.
		w.Header().Set("Retry-After", retryAfterSeconds)
	}
	logger.WithFields(log.Fields{"count": len(res), "source": source, "reason": reason}).Infof("got subtitles")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
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
