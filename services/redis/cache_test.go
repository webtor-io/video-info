package redis

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/webtor-io/video-info/services/osdb"
)

// fakeStore is a redis that remembers what it was told, TTL included.
type fakeStore struct {
	vals map[string][]byte
	ttls map[string]time.Duration
	sets int
}

func newFakeStore() *fakeStore {
	return &fakeStore{vals: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (f *fakeStore) Get(_ context.Context, key string) *redis.StringCmd {
	v, ok := f.vals[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(string(v), nil)
}

func (f *fakeStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	f.vals[key] = value.([]byte)
	f.ttls[key] = ttl
	f.sets++
	return redis.NewStatusResult("OK", nil)
}

// cacheOn builds a cache backed by the fake instead of a redis server.
func cacheOn(key string, st *fakeStore) *Cache {
	return &Cache{key: key, store: func() store { return st }}
}

func sub(id string) osdb.Subtitle {
	var s osdb.Subtitle
	s.Id = id
	return s
}

// Why the wrapper exists at all: gob has no way to say "empty". This pins the
// dependency's behaviour, so if it ever changes the marker can be reconsidered
// instead of being carried forever on trust.
func TestGobFlattensAnEmptySliceToNil(t *testing.T) {
	c := NewCache("k", nil)
	data, err := c.encode([]osdb.Subtitle{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got []osdb.Subtitle
	if err := c.decode(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != nil {
		t.Fatal("gob no longer flattens an empty slice; the Found marker may be redundant")
	}
}

// The marker survives the round trip, so "searched, found nothing" comes back
// as an answer rather than as a miss.
func TestEmptyResultStaysFound(t *testing.T) {
	c := NewCache("k", nil)
	data, err := c.encodeSubtitles([]osdb.Subtitle{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	subs, found := c.decodeSubtitles(data)
	if !found {
		t.Fatal("an empty result must read back as found")
	}
	if len(subs) != 0 {
		t.Fatalf("subtitles=%v", subs)
	}
}

func TestNonEmptyResultRoundTrips(t *testing.T) {
	c := NewCache("k", nil)
	data, err := c.encodeSubtitles([]osdb.Subtitle{sub("1"), sub("2")})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	subs, found := c.decodeSubtitles(data)
	if !found || len(subs) != 2 || subs[1].Id != "2" {
		t.Fatalf("subs=%v found=%v", subs, found)
	}
}

// Entries written before the marker existed are bare slices. They must fail to
// decode rather than read as an empty answer, so GetSubtitles reports a miss
// and the next search overwrites the key: the format migrates itself.
func TestLegacyEntryFailsToDecode(t *testing.T) {
	c := NewCache("k", nil)
	for _, legacy := range [][]osdb.Subtitle{{}, {sub("1")}} {
		data, err := c.encode(legacy)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		subs, found := c.decodeSubtitles(data)
		if found {
			t.Fatalf("a pre-marker entry must read as a miss, got %v", subs)
		}
	}
}

// An empty answer is kept for a shorter time than a real listing: a torrent
// published minutes ago usually has no subtitles yet, and a day-long "nothing
// here" would hide the subtitles uploaded right after it.
//
// This goes through SetSubtitles rather than comparing the two constants: the
// constants can differ while the branch that picks between them is gone.
func TestSetSubtitlesPicksTTLByEmptiness(t *testing.T) {
	cases := []struct {
		name string
		subs []osdb.Subtitle
		want time.Duration
	}{
		{"empty", []osdb.Subtitle{}, emptySubtitlesTTL},
		{"nil", nil, emptySubtitlesTTL},
		{"one track", []osdb.Subtitle{sub("1")}, subtitlesTTL},
	}
	for _, c := range cases {
		st := newFakeStore()
		if err := cacheOn("k", st).SetSubtitles(context.Background(), c.subs); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := st.ttls["ksubsrest"]; got != c.want {
			t.Errorf("%s: ttl=%v want %v", c.name, got, c.want)
		}
	}
	if emptySubtitlesTTL >= subtitlesTTL {
		t.Fatalf("emptySubtitlesTTL=%v subtitlesTTL=%v", emptySubtitlesTTL, subtitlesTTL)
	}
}

// The whole point of I6, through the real Get/Set pair rather than through the
// codec alone: an empty result written to the cache reads back as an answer.
func TestSetThenGetSubtitlesKeepsTheEmptyAnswer(t *testing.T) {
	st := newFakeStore()
	c := cacheOn("k", st)
	if err := c.SetSubtitles(context.Background(), []osdb.Subtitle{}); err != nil {
		t.Fatal(err)
	}
	subs, found, err := c.GetSubtitles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("an empty result must read back as found")
	}
	if len(subs) != 0 {
		t.Fatalf("subs=%v", subs)
	}
}

func TestSetThenGetSubtitlesRoundTrips(t *testing.T) {
	st := newFakeStore()
	c := cacheOn("k", st)
	if err := c.SetSubtitles(context.Background(), []osdb.Subtitle{sub("1"), sub("2")}); err != nil {
		t.Fatal(err)
	}
	subs, found, err := c.GetSubtitles(context.Background())
	if err != nil || !found || len(subs) != 2 || subs[1].Id != "2" {
		t.Fatalf("subs=%v found=%v err=%v", subs, found, err)
	}
}

// A key that was never written is a miss, not an empty answer.
func TestGetSubtitlesOnAMissingKey(t *testing.T) {
	_, found, err := cacheOn("k", newFakeStore()).GetSubtitles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("an absent key must not read as found")
	}
}

// An entry written before the marker existed reads as a miss through the real
// getter, so the next search overwrites it.
func TestGetSubtitlesOnALegacyEntry(t *testing.T) {
	st := newFakeStore()
	c := cacheOn("k", st)
	legacy, err := c.encode([]osdb.Subtitle{sub("1")})
	if err != nil {
		t.Fatal(err)
	}
	st.vals["ksubsrest"] = legacy

	_, found, err := c.GetSubtitles(context.Background())
	if err != nil {
		t.Fatalf("a legacy entry must be a miss, not an error: %v", err)
	}
	if found {
		t.Fatal("a legacy entry must not read as found")
	}
}
