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
	hash    []osdb.Subtitle
	hashErr error
	imdb    []osdb.Subtitle
	imdbErr error
	gotQ    SearchQuery
	calls   []string
}

func (f *fakeSearcher) ByHash(_ context.Context, _ string, _ *redis.Cache, _ bool) ([]osdb.Subtitle, error) {
	f.calls = append(f.calls, "hash")
	return f.hash, f.hashErr
}
func (f *fakeSearcher) ByIMDB(_ context.Context, q SearchQuery, _ *redis.Cache, _ bool) ([]osdb.Subtitle, error) {
	f.calls = append(f.calls, "imdb")
	f.gotQ = q
	return f.imdb, f.imdbErr
}

func one(id string) osdb.Subtitle { var s osdb.Subtitle; s.Id = id; s.Attributes.Language = "en"; return s }

// hashOne mirrors what OpenSubtitles actually returns for a hash search: moviehash_match
// is only ever populated (true) when the query included a moviehash, per brief note on sourceOf.
func hashOne(id string) osdb.Subtitle {
	s := one(id)
	s.Attributes.MoviehashMatch = true
	return s
}

func TestSearchHashWins(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{hashOne("h")}, imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "hash" || len(subs) != 1 || subs[0].Id != "h" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("imdb must not be queried when hash hits: %v", f.calls)
	}
}

func TestSearchFallsBackToIMDBOnEmptyHash(t *testing.T) {
	f := &fakeSearcher{imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1", Season: 2, Episode: 5}, false, nil, log.NewEntry(log.New()))
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
	_, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "imdb" {
		t.Fatalf("src=%q err=%v", src, err)
	}
}

func TestSearchNoIMDBNoFallback(t *testing.T) {
	f := &fakeSearcher{}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "" || len(subs) != 0 || len(f.calls) != 1 {
		t.Fatalf("subs=%v src=%q err=%v calls=%v", subs, src, err, f.calls)
	}
}

func TestCacheKeyIncludesEpisode(t *testing.T) {
	a := cacheKey("hash", "/p", SearchQuery{ImdbID: "tt1", Season: 1, Episode: 1})
	b := cacheKey("hash", "/p", SearchQuery{ImdbID: "tt1", Season: 1, Episode: 2})
	if a == b {
		t.Fatal("cache key must differ per episode")
	}
}
