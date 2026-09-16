package services

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/urfave/cli"
	"github.com/webtor-io/video-info/services/osdb"
)

// memCache is a searchCache that keeps everything in memory, so a search leg can
// be run end to end without redis. It implements the same contract the redis
// cache does: GetSubtitles reports "searched" separately from "found tracks".
type memCache struct {
	mux   sync.Mutex
	hash  uint64
	size  int64
	subs  []osdb.Subtitle
	found bool
	sets  int
}

func (c *memCache) GetHashAndSize(context.Context) (uint64, int64, error) {
	c.mux.Lock()
	defer c.mux.Unlock()
	return c.hash, c.size, nil
}

func (c *memCache) SetHashAndSize(_ context.Context, hash uint64, size int64) error {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.hash, c.size = hash, size
	return nil
}

func (c *memCache) GetSubtitles(context.Context) ([]osdb.Subtitle, bool, error) {
	c.mux.Lock()
	defer c.mux.Unlock()
	return c.subs, c.found, nil
}

func (c *memCache) SetSubtitles(_ context.Context, subs []osdb.Subtitle) error {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.subs, c.found, c.sets = subs, true, c.sets+1
	return nil
}

// osdbClientTo builds a real client aimed at a test server, with pacing off.
func osdbClientTo(t *testing.T, body string) (*osdb.Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String(osdb.OsdbApiURLFlag, srv.URL, "")
	fs.Float64(osdb.OsdbRateFlag, 0, "")
	return osdb.NewClient(cli.NewContext(nil, fs, nil), srv.Client()), &calls
}

// A file with no subtitles is the majority case (53% of listings). Its empty
// result must be cached like any other: the second listing of the same file
// must not reach OpenSubtitles again.
//
// Two Search instances, not two calls on one: SearchPool drops its entry as
// soon as the in-flight search returns, so the second page view builds a new
// one and the only thing between them is the cache.
func TestEmptySearchResultIsCached(t *testing.T) {
	cl, calls := osdbClientTo(t, `{"total_pages":1,"total_count":0,"page":1,"data":[]}`)
	c := &memCache{hash: 0xdeadbeef, size: 1 << 30}
	hp := NewHashPool()

	for i := 0; i < 2; i++ {
		subs, err := NewSearch("http://seeder/f.mkv", hp, cl, c).Get(context.Background(), false)
		if err != nil {
			t.Fatalf("search %d: %v", i+1, err)
		}
		if len(subs) != 0 {
			t.Fatalf("search %d: subs=%v", i+1, subs)
		}
	}
	if *calls != 1 {
		t.Fatalf("upstream calls=%d want 1: an empty result is an answer, not a miss", *calls)
	}
	if c.sets != 1 {
		t.Fatalf("cache writes=%d want 1", c.sets)
	}
}

func TestNonEmptySearchResultIsCached(t *testing.T) {
	cl, calls := osdbClientTo(t, `{"data":[{"id":"1","attributes":{"language":"en"}}]}`)
	c := &memCache{hash: 0xdeadbeef, size: 1 << 30}
	hp := NewHashPool()

	for i := 0; i < 2; i++ {
		subs, err := NewSearch("http://seeder/f.mkv", hp, cl, c).Get(context.Background(), false)
		if err != nil {
			t.Fatalf("search %d: %v", i+1, err)
		}
		if len(subs) != 1 || subs[0].Id != "1" {
			t.Fatalf("search %d: subs=%v", i+1, subs)
		}
	}
	if *calls != 1 {
		t.Fatalf("upstream calls=%d want 1", *calls)
	}
}

// purge=true is the escape hatch: a cached empty answer must not survive it.
// (Driven through the IMDb leg because purging the hash leg also purges the
// moviehash, which would send the test at a real seeder.)
func TestPurgeBypassesTheCachedEmptyResult(t *testing.T) {
	cl, calls := osdbClientTo(t, `{"data":[]}`)
	c := &memCache{}
	q := SearchQuery{ImdbID: "tt0109424"}

	if _, err := NewIMDBSearch(q, cl, c).Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewIMDBSearch(q, cl, c).Get(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("upstream calls=%d want 2", *calls)
	}
}

// The same rule on the IMDb leg: a title with no subtitles is not searched again.
func TestEmptyIMDBSearchResultIsCached(t *testing.T) {
	cl, calls := osdbClientTo(t, `{"data":[]}`)
	c := &memCache{}
	q := SearchQuery{ImdbID: "tt0109424"}

	for i := 0; i < 2; i++ {
		subs, err := NewIMDBSearch(q, cl, c).Get(context.Background(), false)
		if err != nil {
			t.Fatalf("search %d: %v", i+1, err)
		}
		if len(subs) != 0 {
			t.Fatalf("search %d: subs=%v", i+1, subs)
		}
	}
	if *calls != 1 {
		t.Fatalf("upstream calls=%d want 1", *calls)
	}
}
