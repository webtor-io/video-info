package services

import (
	"context"
	"errors"
	"testing"

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

func one(id string) osdb.Subtitle { var s osdb.Subtitle; s.Id = id; s.Attributes.Language = "en"; return s }

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

func testLogger() *log.Entry {
	l := log.New()
	l.SetOutput(discard{})
	return log.NewEntry(l)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestSearchHashWins(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{hashOne("h")}, imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
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
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
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
	_, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, hc, ic, testLogger())
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
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1", Season: 2, Episode: 5}, false, nil, nil, testLogger())
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
	_, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
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
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, nil, testLogger())
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
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{}, false, nil, nil, testLogger())
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
