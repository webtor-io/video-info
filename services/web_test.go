package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

// The source is the leg that produced the list, not a property of the tracks:
// a hash hit with no moviehash_match flag is still "hash".
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
