package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/webtor-io/video-info/services/osdb"
	"github.com/webtor-io/video-info/services/redis"
)

func cacheID(c subtitleCache) string { return fmt.Sprintf("%p", c) }

// boundTo builds an already-inited IMDBSearch whose value names the very cache
// instance it is bound to, so a caller can tell which stored search answered it
// without touching redis.
func boundTo(q SearchQuery, cl *osdb.Client, c subtitleCache) *IMDBSearch {
	var s osdb.Subtitle
	s.Id = cacheID(c)
	return &IMDBSearch{q: q, cl: cl, cache: c, inited: true, value: []osdb.Subtitle{s}}
}

func answeredBy(t *testing.T, subs []osdb.Subtitle) string {
	t.Helper()
	if len(subs) != 1 {
		t.Fatalf("want one subtitle, got %v", subs)
	}
	return subs[0].Id
}

// The in-process dedup map must be keyed by the full imdb cache key that the
// caller passes, not by the query: a stored IMDBSearch captures its caller's
// cache, so two files of the same title keyed only by the query would share one
// search bound to whichever cache arrived first.
func TestIMDBSearchPoolKeysByCacheKey(t *testing.T) {
	orig := newIMDBSearch
	t.Cleanup(func() { newIMDBSearch = orig })
	newIMDBSearch = boundTo

	p := NewIMDBSearchPool(nil)
	q := SearchQuery{ImdbID: "tt1"}
	keyA, keyB := "fileA:imdb:1::", "fileB:imdb:1::"
	// The cache pool hands out a fresh instance per call, so equal keys still
	// mean distinct pointers.
	c1, c1b, c2 := redis.NewCache(keyA, nil), redis.NewCache(keyA, nil), redis.NewCache(keyB, nil)

	// A search for file A is in flight: the pool holds it until its Get returns.
	p.sm.Store(keyA, boundTo(q, nil, c1))

	// Same key: joins the in-flight search instead of starting its own.
	subs, err := p.Get(context.Background(), keyA, q, c1b, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := answeredBy(t, subs); got != cacheID(c1) {
		t.Fatalf("same key must join the in-flight search (%v), got %v", cacheID(c1), got)
	}

	// Same query, other file: its own search, bound to its own cache.
	subs, err = p.Get(context.Background(), keyB, q, c2, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := answeredBy(t, subs); got != cacheID(c2) {
		t.Fatalf("another file must get its own search (%v), got %v", cacheID(c2), got)
	}
}
