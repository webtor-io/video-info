package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	pkgerrors "github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/video-info/services/osdb"
	"github.com/webtor-io/video-info/services/redis"
)

type fakeSearcher struct {
	hash      []osdb.Subtitle
	hashErr   error
	imdb      []osdb.Subtitle
	imdbErr   error
	gotQ      SearchQuery
	calls     []string
	hashCache *redis.Cache
	imdbCache *redis.Cache
}

func (f *fakeSearcher) ByHash(_ context.Context, _ string, c *redis.Cache, _ bool) ([]osdb.Subtitle, error) {
	f.calls = append(f.calls, "hash")
	f.hashCache = c
	return f.hash, f.hashErr
}
func (f *fakeSearcher) ByIMDB(_ context.Context, q SearchQuery, c *redis.Cache, _ bool) ([]osdb.Subtitle, error) {
	f.calls = append(f.calls, "imdb")
	f.imdbCache = c
	f.gotQ = q
	return f.imdb, f.imdbErr
}

func one(id string) osdb.Subtitle {
	var s osdb.Subtitle
	s.Id = id
	s.Attributes.Language = "en"
	return s
}

// hashOne mirrors what OpenSubtitles actually returns for a hash search: moviehash_match
// is only ever populated (true) when the query included a moviehash.
func hashOne(id string) osdb.Subtitle {
	s := one(id)
	s.Attributes.MoviehashMatch = true
	return s
}

func callsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The handlers log through the package-level logrus logger; keep test output clean.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func testLogger() *log.Entry {
	l := log.New()
	l.SetOutput(io.Discard)
	return log.NewEntry(l)
}

func TestSearchHashWins(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{hashOne("h")}, imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil || src != "hash" || len(subs) != 1 || subs[0].Id != "h" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("imdb must not be queried when hash hits: %v", f.calls)
	}
}

// The leg name search returns is the leg, not a property of the tracks: a hash
// hit with no moviehash_match flag still came from the hash leg. It is used for
// logging only; what the client is told per track is checked by
// TestSubtitlesJSONSourceIsPerTrack.
func TestSearchHashWinsWithoutMoviehashFlag(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{one("h")}, imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil || src != "hash" || len(subs) != 1 || subs[0].Id != "h" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if !callsEqual(f.calls, []string{"hash"}) {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestSearchUsesSeparateCachePerLeg(t *testing.T) {
	hc := redis.NewCache("hash-key", nil)
	ic := redis.NewCache("imdb-key", nil)
	f := &fakeSearcher{imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	_, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, hc, ic, testLogger())
	if err != nil || src != "imdb" {
		t.Fatalf("src=%q err=%v", src, err)
	}
	if f.hashCache != hc {
		t.Fatalf("hash leg got cache %p, want %p", f.hashCache, hc)
	}
	if f.imdbCache != ic {
		t.Fatalf("imdb leg got cache %p, want %p", f.imdbCache, ic)
	}
}

func TestSearchFallsBackToIMDBOnEmptyHash(t *testing.T) {
	f := &fakeSearcher{imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1", Season: 2, Episode: 5}, false, nil, nil, testLogger())
	if err != nil || src != "imdb" || len(subs) != 1 || subs[0].Id != "i" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if f.gotQ.Season != 2 || f.gotQ.Episode != 5 {
		t.Fatalf("season/episode not forwarded: %+v", f.gotQ)
	}
}

func TestSearchFallsBackToIMDBOnHashError(t *testing.T) {
	f := &fakeSearcher{hashErr: errors.New("boom"), imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	_, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil || src != "imdb" {
		t.Fatalf("src=%q err=%v", src, err)
	}
	if !callsEqual(f.calls, []string{"hash", "imdb"}) {
		t.Fatalf("calls=%v", f.calls)
	}
}

// Every hash hit is dropped by RankSubtitles, so the hash leg produced nothing
// usable and the imdb leg must still run.
func TestSearchFallsBackWhenHashHitsAreAllFiltered(t *testing.T) {
	a, b := hashOne("h1"), hashOne("h2")
	a.Attributes.AiTranslated = true
	b.Attributes.AiTranslated = true
	f := &fakeSearcher{hash: []osdb.Subtitle{a, b}, imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil || src != "imdb" || len(subs) != 1 || subs[0].Id != "i" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if !callsEqual(f.calls, []string{"hash", "imdb"}) {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestSearchNoIMDBNoFallback(t *testing.T) {
	f := &fakeSearcher{}
	w := &Web{searcher: f}
	subs, src, _, err := w.search(context.Background(), "http://src", SearchQuery{}, false, nil, nil, testLogger())
	if err != nil || src != "" || len(subs) != 0 || len(f.calls) != 1 {
		t.Fatalf("subs=%v src=%q err=%v calls=%v", subs, src, err, f.calls)
	}
}

func TestCacheKeyIncludesEpisode(t *testing.T) {
	a := imdbCacheKey("hash", "/p", SearchQuery{ImdbID: "tt1", Season: 1, Episode: 1})
	b := imdbCacheKey("hash", "/p", SearchQuery{ImdbID: "tt1", Season: 1, Episode: 2})
	if a == b {
		t.Fatal("imdb cache key must differ per episode")
	}
}

// The hash/size cache lives under the hash key, so hints must not fragment it.
func TestHashCacheKeyIgnoresHints(t *testing.T) {
	if got, want := hashCacheKey("hash", "/p"), "hash/p"; got != want {
		t.Fatalf("hashCacheKey=%q want %q", got, want)
	}
	if hashCacheKey("hash", "/p") == imdbCacheKey("hash", "/p", SearchQuery{ImdbID: "tt1"}) {
		t.Fatal("hash and imdb keys must differ")
	}
}

// A malformed imdb-id must never reach the client: it would be spliced into
// the upstream query and collide as a pool key.
func TestSearchIgnoresInvalidImdbID(t *testing.T) {
	f := &fakeSearcher{imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, _, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "1&x=y"}, false, nil, nil, testLogger())
	if err != nil || src != "" || len(subs) != 0 {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if !callsEqual(f.calls, []string{"hash"}) {
		t.Fatalf("imdb leg must not run for an invalid id: %v", f.calls)
	}
}

func TestRedactURLDropsQuery(t *testing.T) {
	cases := map[string]string{
		"http://seeder/f.mkv?token=secret.jwt.value&x=1": "http://seeder/f.mkv",
		"http://seeder/f.mkv":                            "http://seeder/f.mkv",
		"":                                               "",
		"://nope":                                        "",
	}
	for in, want := range cases {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseSearchQueryHalfPair(t *testing.T) {
	cases := map[string][2]int{
		"?imdb-id=tt1&season=2":           {0, 0},
		"?imdb-id=tt1&episode=5":          {0, 0},
		"?imdb-id=tt1&season=0&episode=5": {0, 0},
		"?imdb-id=tt1&season=2&episode=5": {2, 5},
		"?imdb-id=tt1":                    {0, 0},
		"?imdb-id=tt1&season=x&episode=5": {0, 0},
	}
	for qs, want := range cases {
		q := parseSearchQuery(httptest.NewRequest("GET", "/subtitles.json"+qs, nil))
		if q.Season != want[0] || q.Episode != want[1] {
			t.Errorf("%s: season=%d episode=%d want %d/%d", qs, q.Season, q.Episode, want[0], want[1])
		}
	}
}

func rich(id string) osdb.Subtitle {
	s := hashOne(id)
	s.Attributes.Release = "GROUP.1080p"
	s.Attributes.Fps = 23.976
	s.Attributes.HearingImpaired = true
	s.Attributes.DownloadCount = 5
	return s
}

func TestSubtitlesJSONContract(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{rich("1"), hashOne("2")}}
	w := &Web{searcher: f, cachePool: redis.NewCachePool(nil)}

	r := httptest.NewRequest("GET", "/subtitles.json", nil)
	r.Header.Set("X-Source-Url", "http://seeder/f.mkv?token=secret")
	r.Header.Set("X-Info-Hash", "abc")
	r.Header.Set("X-Path", "/f.mkv")
	rr := httptest.NewRecorder()
	w.handleSubtitlesJSON(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q", ct)
	}
	var items []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("body=%q err=%v", rr.Body.String(), err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%v", items)
	}
	for _, it := range items {
		for _, k := range []string{"srclang", "label", "src", "format", "id", "source"} {
			if _, ok := it[k]; !ok {
				t.Fatalf("missing %q in %v", k, it)
			}
		}
		if it["source"] != "hash" {
			t.Fatalf("source=%v want hash", it["source"])
		}
		if want := "/opensubtitles/" + it["id"].(string) + ".vtt"; it["src"] != want {
			t.Fatalf("src=%v want %v", it["src"], want)
		}
		if it["srclang"] != "en" || it["label"] != "English" {
			t.Fatalf("lang fields %v", it)
		}
	}
	// Optional fields are present when set...
	first := items[0]
	if first["id"] != "1" || first["release"] != "GROUP.1080p" || first["fps"] != 23.976 ||
		first["hi"] != true || first["downloads"] != float64(5) {
		t.Fatalf("optional fields not carried: %v", first)
	}
	// ...and omitted when zero.
	for _, k := range []string{"release", "fps", "hi", "downloads"} {
		if _, ok := items[1][k]; ok {
			t.Fatalf("zero %q must be omitted: %v", k, items[1])
		}
	}
}

func TestSubtitlesJSONEmptyEncodesAsArray(t *testing.T) {
	w := &Web{searcher: &fakeSearcher{}, cachePool: redis.NewCachePool(nil)}
	r := httptest.NewRequest("GET", "/subtitles.json", nil)
	r.Header.Set("X-Source-Url", "http://seeder/f.mkv")
	rr := httptest.NewRecorder()
	w.handleSubtitlesJSON(rr, r)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
}

// timeoutError is what a stalled read from a cold seeder looks like by the time
// it reaches the handler: an error that only announces itself through Timeout().
type timeoutError struct{}

func (timeoutError) Error() string { return "read tcp: i/o timeout" }
func (timeoutError) Timeout() bool { return true }

func TestFailureReasonClassifies(t *testing.T) {
	cases := []struct {
		name string
		leg  string
		err  error
		want string
	}{
		{"no error", "hash", nil, ""},
		{"cancelled", "hash", context.Canceled, "client_gone"},
		{"cancelled wrapped", "hash", pkgerrors.Wrap(context.Canceled, "failed to get hash"), "client_gone"},
		{"deadline", "hash", context.DeadlineExceeded, "hash_timeout"},
		{"net timeout wrapped", "hash", pkgerrors.Wrap(timeoutError{}, "failed to read head block"), "hash_timeout"},
		{"other", "hash", errors.New("boom"), "hash_error"},
		{"imdb leg", "imdb", errors.New("boom"), "imdb_error"},
	}
	for _, c := range cases {
		if got := failureReason(c.leg, c.err); got != c.want {
			t.Errorf("%s: failureReason(%q, %v)=%q want %q", c.name, c.leg, c.err, got, c.want)
		}
	}
}

// A failing hash leg means "not ready", not "no such resource". The reply keeps
// the shape of a real listing (JSON array, same content type) and carries
// Retry-After, because web-ui parses the body without checking the status and a
// 404 also teaches the browser and the CDN that the track list does not exist.
func TestSubtitlesJSONHashFailureAnswersEmptyList(t *testing.T) {
	w := &Web{
		searcher:  &fakeSearcher{hashErr: pkgerrors.Wrap(timeoutError{}, "failed to read head block")},
		cachePool: redis.NewCachePool(nil),
	}
	srv := httptest.NewServer(http.HandlerFunc(w.handleSubtitlesJSON))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest("GET", srv.URL+"/subtitles.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Source-Url", "http://seeder/f.mkv?token=secret")
	req.Header.Set("X-Info-Hash", "abc")
	req.Header.Set("X-Path", "/f.mkv")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", res.StatusCode)
	}
	if got := res.Header.Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After=%q want 5", got)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q", ct)
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if len(items) != 0 {
		t.Fatalf("items=%v", items)
	}
}

// The only genuine 404 left: the request names no file and no title, so there
// is no resource to report on.
func TestSubtitlesJSONWithoutFileOrTitleIs404(t *testing.T) {
	w := &Web{searcher: &fakeSearcher{}, cachePool: redis.NewCachePool(nil)}
	r := httptest.NewRequest("GET", "/subtitles.json", nil)
	rr := httptest.NewRecorder()
	w.handleSubtitlesJSON(rr, r)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
}

// Negative control for the header: a file that really has no subtitles answers
// the same empty list, but without Retry-After — there is nothing to come back
// for, and asking every viewer to retry every five seconds would be a lie.
func TestSubtitlesJSONEmptyResultHasNoRetryAfter(t *testing.T) {
	w := &Web{searcher: &fakeSearcher{}, cachePool: redis.NewCachePool(nil)}
	r := httptest.NewRequest("GET", "/subtitles.json", nil)
	r.Header.Set("X-Source-Url", "http://seeder/f.mkv")
	rr := httptest.NewRecorder()
	w.handleSubtitlesJSON(rr, r)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After=%q want none", got)
	}
}

// The reason travels with an empty listing so the log can split the causes.
func TestSearchReportsFailureReason(t *testing.T) {
	f := &fakeSearcher{hashErr: pkgerrors.Wrap(context.Canceled, "failed to get hash")}
	w := &Web{searcher: f}
	subs, src, reason, err := w.search(context.Background(), "http://src", SearchQuery{}, false, nil, nil, testLogger())
	if err != nil || len(subs) != 0 || src != "" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if reason != "client_gone" {
		t.Fatalf("reason=%q want client_gone", reason)
	}
}

// A hash leg that failed still counts as unfinished when the imdb leg turned up
// nothing, so the viewer is told to come back rather than told there is nothing.
func TestSearchKeepsHashReasonWhenIMDBIsEmpty(t *testing.T) {
	f := &fakeSearcher{hashErr: errors.New("boom")}
	w := &Web{searcher: f}
	_, _, reason, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if reason != "hash_error" {
		t.Fatalf("reason=%q want hash_error", reason)
	}
	if !callsEqual(f.calls, []string{"hash", "imdb"}) {
		t.Fatalf("calls=%v", f.calls)
	}
}

// fakeFetcher stands in for SubsPool: it records which leg's cache the body was
// asked from, which is how the per-leg body cache is checked without redis.
type fakeFetcher struct {
	got   *osdb.Subtitle
	cache *redis.Cache
	body  []byte
	err   error
}

func (f *fakeFetcher) Get(_ context.Context, sub *osdb.Subtitle, _ string, c *redis.Cache, _ bool, _ *log.Entry) ([]byte, error) {
	f.got, f.cache = sub, c
	return f.body, f.err
}

func numbered(id, lang string) osdb.Subtitle {
	s := one(id)
	s.Attributes.Language = lang
	return s
}

func subtitleRequest(path string) *http.Request {
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("X-Source-Url", "http://seeder/f.mkv?token=secret")
	r.Header.Set("X-Info-Hash", "abc")
	r.Header.Set("X-Path", "/f.mkv")
	return r
}

// The listing the viewer clicked came from the imdb leg because the hash leg
// was still cold. By the time the track is fetched the hash leg answers, and a
// re-run search would return only its list — the clicked id must still resolve.
func TestHandleSubtitleFindsIMDBTrackAfterHashLegAppears(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{hashOne("11")}, imdb: []osdb.Subtitle{numbered("22", "fr")}}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/22.vtt?imdb-id=tt1"))

	if rr.Code != http.StatusOK || rr.Body.String() != "WEBVTT\n" {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
	if fetch.got == nil || fetch.got.Id != "22" {
		t.Fatalf("fetched %+v", fetch.got)
	}
	if !callsEqual(f.calls, []string{"hash", "imdb"}) {
		t.Fatalf("both legs must be asked before giving up: %v", f.calls)
	}
	// the body of an imdb-leg track lives under the imdb leg's cache key
	if want := imdbCacheKey("abc", "/f.mkv", SearchQuery{ImdbID: "tt1"}); fetch.cache.Key() != want {
		t.Fatalf("cache=%q want %q", fetch.cache.Key(), want)
	}
}

func TestHandleSubtitleFindsHashTrack(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{hashOne("11")}, imdb: []osdb.Subtitle{numbered("22", "fr")}}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/11.vtt?imdb-id=tt1"))

	if rr.Code != http.StatusOK || fetch.got == nil || fetch.got.Id != "11" {
		t.Fatalf("status=%d fetched=%+v", rr.Code, fetch.got)
	}
	if !callsEqual(f.calls, []string{"hash"}) {
		t.Fatalf("the imdb leg must not be asked once the id resolved: %v", f.calls)
	}
	if want := hashCacheKey("abc", "/f.mkv"); fetch.cache.Key() != want {
		t.Fatalf("cache=%q want %q", fetch.cache.Key(), want)
	}
}

// A track the hash leg listed still resolves when the hash leg has meanwhile
// gone cold and only the imdb leg answers — the mirror image of the first case.
func TestHandleSubtitleFindsHashTrackThroughIMDBLegAfterHashFails(t *testing.T) {
	f := &fakeSearcher{hashErr: errors.New("boom"), imdb: []osdb.Subtitle{numbered("11", "en")}}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/11.vtt?imdb-id=tt1"))

	if rr.Code != http.StatusOK || fetch.got == nil || fetch.got.Id != "11" {
		t.Fatalf("status=%d fetched=%+v", rr.Code, fetch.got)
	}
}

func TestHandleSubtitleUnknownIDIs404(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{hashOne("11")}, imdb: []osdb.Subtitle{numbered("22", "fr")}}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/99.vtt?imdb-id=tt1"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
	// Both legs answered, so this is final: no Retry-After, no 503.
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After=%q want none", got)
	}
	if fetch.got != nil {
		t.Fatalf("nothing must be fetched: %+v", fetch.got)
	}
	if !callsEqual(f.calls, []string{"hash", "imdb"}) {
		t.Fatalf("calls=%v", f.calls)
	}
}

// The lookup is over the candidates, not the listing: a track beyond the
// per-language cap of three is not shown, but its id is still valid — a viewer
// can hold it from a listing made when the ranking put it higher.
func TestHandleSubtitleResolvesBeyondPerLangCap(t *testing.T) {
	var hash []osdb.Subtitle
	for _, id := range []string{"11", "12", "13", "14"} {
		hash = append(hash, hashOne(id))
	}
	f := &fakeSearcher{hash: hash}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/14.vtt"))

	if rr.Code != http.StatusOK || fetch.got == nil || fetch.got.Id != "14" {
		t.Fatalf("status=%d fetched=%+v", rr.Code, fetch.got)
	}
}

// A track the ranking drops as useless for full dialogue never reaches a
// listing, so its id must not resolve either.
func TestHandleSubtitleDoesNotResolveFilteredTrack(t *testing.T) {
	ai := hashOne("11")
	ai.Attributes.AiTranslated = true
	f := &fakeSearcher{hash: []osdb.Subtitle{ai}}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/11.vtt"))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
}

func TestHandleSubtitleWithoutFileOrTitleIs404(t *testing.T) {
	f := &fakeSearcher{}
	w := &Web{searcher: f, subsPool: &fakeFetcher{}, cachePool: redis.NewCachePool(nil)}
	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, httptest.NewRequest("GET", "/opensubtitles/11.vtt", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rr.Code)
	}
	if len(f.calls) != 0 {
		t.Fatalf("no leg must run: %v", f.calls)
	}
}

// source describes the track, not the leg. A moviehash search returns the tracks
// of the movie, and only some of them actually matched the hash; the rest are
// no better synced than an imdb result and must not be labelled as if they were.
func TestSubtitlesJSONSourceIsPerTrack(t *testing.T) {
	matched, unmatched := hashOne("1"), one("2")
	f := &fakeSearcher{hash: []osdb.Subtitle{matched, unmatched}}
	w := &Web{searcher: f, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitlesJSON(rr, subtitleRequest("/subtitles.json"))

	var items []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("body=%q err=%v", rr.Body.String(), err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%v", items)
	}
	if items[0]["source"] != "hash" || items[0]["moviehash_match"] != true {
		t.Fatalf("hash-matched track: %v", items[0])
	}
	if items[1]["source"] != "imdb" || items[1]["moviehash_match"] != false {
		t.Fatalf("track of the same movie, not of this file: %v", items[1])
	}
}

// The flag is reported even when it agrees with source, so a consumer never has
// to infer it from the label.
func TestSubtitlesJSONCarriesMoviehashMatchFromIMDBLeg(t *testing.T) {
	f := &fakeSearcher{imdb: []osdb.Subtitle{one("2")}}
	w := &Web{searcher: f, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitlesJSON(rr, subtitleRequest("/subtitles.json?imdb-id=tt1"))

	var items []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("body=%q err=%v", rr.Body.String(), err)
	}
	if len(items) != 1 || items[0]["source"] != "imdb" {
		t.Fatalf("items=%v", items)
	}
	if v, ok := items[0]["moviehash_match"]; !ok || v != false {
		t.Fatalf("moviehash_match must be present and false: %v", items[0])
	}
}

// The .vtt path makes the same distinction the listing makes: a leg that failed
// means the track may exist and could not be read yet. On a cold torrent both
// legs can be unavailable at once — the hash entry expired, the seeder is cold,
// and the request carries no imdb-id — and 404 would tell the browser and the
// CDN that the track is gone for good.
func TestHandleSubtitleLegFailureIs503(t *testing.T) {
	f := &fakeSearcher{hashErr: pkgerrors.Wrap(timeoutError{}, "failed to read head block")}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	srv := httptest.NewServer(http.HandlerFunc(w.handleSubtitle))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest("GET", srv.URL+"/opensubtitles/11.vtt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Source-Url", "http://seeder/f.mkv?token=secret")
	req.Header.Set("X-Info-Hash", "abc")
	req.Header.Set("X-Path", "/f.mkv")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503", res.StatusCode)
	}
	if got := res.Header.Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After=%q want 5", got)
	}
	// the player reads this response, and the proxy does not add to
	// Expose-Headers on its own
	if got := res.Header.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Retry-After") {
		t.Fatalf("Access-Control-Expose-Headers=%q", got)
	}
	if fetch.got != nil {
		t.Fatalf("nothing must be fetched: %+v", fetch.got)
	}
}

// A leg that failed while the *other* leg resolved the id is not a failure at
// all: the track is served, no 503.
func TestHandleSubtitleLegFailureWithAResultIs200(t *testing.T) {
	f := &fakeSearcher{hashErr: errors.New("boom"), imdb: []osdb.Subtitle{numbered("11", "en")}}
	fetch := &fakeFetcher{body: []byte("WEBVTT\n")}
	w := &Web{searcher: f, subsPool: fetch, cachePool: redis.NewCachePool(nil)}

	rr := httptest.NewRecorder()
	w.handleSubtitle(rr, subtitleRequest("/opensubtitles/11.vtt?imdb-id=tt1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After=%q want none", got)
	}
}

const leakToken = "eyJhbGciOiJIUzI1NiJ9.leaked-token-value"

// seederTimeout is what the head/tail read actually returns: the dependency
// hands back http.Client.Do's *url.Error unwrapped, and its Error() prints the
// full URL, query and all.
func seederTimeout() error {
	return pkgerrors.Wrap(pkgerrors.Wrap(&url.Error{
		Op:  "Get",
		URL: "http://seeder/f.mkv?token=" + leakToken,
		Err: context.DeadlineExceeded,
	}, "failed to read chunk"), "failed to read head block")
}

func TestRedactErrStripsTheQueryFromURLsInTheMessage(t *testing.T) {
	got := redactErr(seederTimeout()).Error()
	if strings.Contains(got, leakToken) {
		t.Fatalf("token survived redaction: %q", got)
	}
	// the location is what makes the line useful; only the query goes
	if !strings.Contains(got, "http://seeder/f.mkv") {
		t.Fatalf("location lost: %q", got)
	}
	for _, want := range []string{"failed to read head block", "context deadline exceeded"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message lost %q: %q", want, got)
		}
	}
	if redactErr(nil) != nil {
		t.Fatal("redactErr(nil) must stay nil")
	}
}

// A second "?" inside the query must not leave the first segment in the clear.
func TestRedactErrStripsEverythingAfterTheFirstQuestionMark(t *testing.T) {
	got := redactErr(errors.New("GET http://seeder/f.mkv?token=" + leakToken + "?x=1: boom")).Error()
	if strings.Contains(got, leakToken) || strings.Contains(got, "token=") {
		t.Fatalf("query segment survived: %q", got)
	}
}

func TestRedactErrLeavesAnErrorWithoutAURLAlone(t *testing.T) {
	if got := redactErr(errors.New("boom")).Error(); got != "boom" {
		t.Fatalf("got %q", got)
	}
}

// captureLog points the package logger, which the handlers use, at a buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(io.Discard) })
	return &buf
}

// The seeder URL reaches the log twice: as a field, which redactURL has always
// cleaned, and inside the error, which it did not. Both handlers log the same
// failing legs, so both are checked.
func TestHandlersDoNotLogTheSeederToken(t *testing.T) {
	cases := map[string]func(*Web, http.ResponseWriter, *http.Request){
		"listing": func(w *Web, rw http.ResponseWriter, r *http.Request) { w.handleSubtitlesJSON(rw, r) },
		"track":   func(w *Web, rw http.ResponseWriter, r *http.Request) { w.handleSubtitle(rw, r) },
	}
	paths := map[string]string{
		"listing": "/subtitles.json?imdb-id=tt1",
		"track":   "/opensubtitles/11.vtt?imdb-id=tt1",
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			buf := captureLog(t)
			// both legs fail, so every WithError site on the path is exercised
			f := &fakeSearcher{hashErr: seederTimeout(), imdbErr: seederTimeout()}
			w := &Web{searcher: f, subsPool: &fakeFetcher{}, cachePool: redis.NewCachePool(nil)}
			r := httptest.NewRequest("GET", paths[name], nil)
			r.Header.Set("X-Source-Url", "http://seeder/f.mkv?token="+leakToken)
			r.Header.Set("X-Info-Hash", "abc")
			r.Header.Set("X-Path", "/f.mkv")
			call(w, httptest.NewRecorder(), r)

			out := buf.String()
			if out == "" {
				t.Fatal("nothing logged; the test would pass vacuously")
			}
			if strings.Contains(out, leakToken) {
				t.Fatalf("token in log: %s", out)
			}
			if !strings.Contains(out, "seeder/f.mkv") {
				t.Fatalf("the location must survive, only the query goes: %s", out)
			}
		})
	}
}

func TestJoinReasons(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "", ""},
		{"hash_timeout", "", "hash_timeout"},
		{"", "imdb_error", "imdb_error"},
		{"hash_timeout", "imdb_error", "hash_timeout+imdb_error"},
		// one cancelled context fails both legs: named once, not twice
		{"client_gone", "client_gone", "client_gone"},
	}
	for _, c := range cases {
		if got := joinReasons(c.a, c.b); got != c.want {
			t.Errorf("joinReasons(%q,%q)=%q want %q", c.a, c.b, got, c.want)
		}
	}
}

// Both legs down at once is the case worth seeing in the split, so neither
// cause may overwrite the other.
func TestSearchKeepsBothLegReasons(t *testing.T) {
	f := &fakeSearcher{
		hashErr: pkgerrors.Wrap(timeoutError{}, "failed to read head block"),
		imdbErr: errors.New("boom"),
	}
	w := &Web{searcher: f}
	_, _, reason, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if reason != "hash_timeout+imdb_error" {
		t.Fatalf("reason=%q want hash_timeout+imdb_error", reason)
	}
}

func TestFindTrackKeepsBothLegReasons(t *testing.T) {
	f := &fakeSearcher{
		hashErr: pkgerrors.Wrap(timeoutError{}, "failed to read head block"),
		imdbErr: context.Canceled,
	}
	w := &Web{searcher: f}
	sub, _, reason := w.findTrack(context.Background(), "11", "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if sub != nil {
		t.Fatalf("sub=%+v", sub)
	}
	if reason != "hash_timeout+client_gone" {
		t.Fatalf("reason=%q want hash_timeout+client_gone", reason)
	}
}

// One leg down still reports one cause, not a dangling separator.
func TestSearchSingleLegReasonIsNotJoined(t *testing.T) {
	f := &fakeSearcher{hashErr: errors.New("boom"), imdb: []osdb.Subtitle{}}
	w := &Web{searcher: f}
	_, _, reason, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if reason != "hash_error" {
		t.Fatalf("reason=%q want hash_error", reason)
	}
}
