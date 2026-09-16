package redis

import (
	"testing"

	"github.com/webtor-io/video-info/services/osdb"
)

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
// published minutes ago usually has no subtitles yet.
func TestEmptyResultHasShorterTTL(t *testing.T) {
	if emptySubtitlesTTL >= subtitlesTTL {
		t.Fatalf("emptySubtitlesTTL=%v subtitlesTTL=%v", emptySubtitlesTTL, subtitlesTTL)
	}
}
