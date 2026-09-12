package services

import (
	"testing"

	"github.com/webtor-io/video-info/services/osdb"
)

func sub(id, lang string, dl int, hashMatch, trusted, foreign, ai bool) osdb.Subtitle {
	var s osdb.Subtitle
	s.Id = id
	s.Attributes.Language = lang
	s.Attributes.DownloadCount = dl
	s.Attributes.MoviehashMatch = hashMatch
	s.Attributes.FromTrusted = trusted
	s.Attributes.ForeignPartsOnly = foreign
	s.Attributes.AiTranslated = ai
	return s
}

func ids(subs []osdb.Subtitle) []string {
	var r []string
	for _, s := range subs {
		r = append(r, s.Id)
	}
	return r
}

func TestRankSubtitlesOrderAndCap(t *testing.T) {
	in := []osdb.Subtitle{
		sub("en-low", "en", 10, false, false, false, false),
		sub("en-trusted", "en", 5, false, true, false, false),
		sub("en-hash", "en", 1, true, false, false, false),
		sub("en-high", "en", 900, false, false, false, false),
		sub("en-foreign", "en", 9999, false, false, true, false),
		sub("pt-ai", "pt", 9999, false, false, false, true),
		sub("pt-one", "pt", 3, false, false, false, false),
	}
	got := ids(RankSubtitles(in, 3))
	want := []string{"en-hash", "en-trusted", "en-high", "pt-one"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRankSubtitlesEmpty(t *testing.T) {
	if got := RankSubtitles(nil, 3); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
