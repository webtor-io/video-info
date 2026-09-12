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

func TestRankSubtitlesPerLangCases(t *testing.T) {
	cases := []struct {
		name    string
		in      []osdb.Subtitle
		perLang int
		want    []string
	}{
		{
			name: "perLang 0 disables the cap",
			in: []osdb.Subtitle{
				sub("en-1", "en", 30, false, false, false, false),
				sub("en-2", "en", 20, false, false, false, false),
				sub("en-3", "en", 10, false, false, false, false),
				sub("en-4", "en", 5, false, false, false, false),
			},
			perLang: 0,
			want:    []string{"en-1", "en-2", "en-3", "en-4"},
		},
		{
			name: "negative perLang disables the cap",
			in: []osdb.Subtitle{
				sub("en-1", "en", 30, false, false, false, false),
				sub("en-2", "en", 20, false, false, false, false),
			},
			perLang: -1,
			want:    []string{"en-1", "en-2"},
		},
		{
			name: "a language whose every candidate is filtered is absent",
			in: []osdb.Subtitle{
				sub("en-ok", "en", 1, false, false, false, false),
				sub("de-forced", "de", 900, false, false, true, false),
				sub("de-ai", "de", 800, false, false, false, true),
			},
			perLang: 3,
			want:    []string{"en-ok"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ids(RankSubtitles(c.in, c.perLang))
			if len(got) != len(c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("got %v want %v", got, c.want)
				}
			}
		})
	}
}
