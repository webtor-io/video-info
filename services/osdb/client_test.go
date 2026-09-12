package osdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{apiURL: srv.URL, cl: srv.Client()}, &seen
}

func okEmpty(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"total_pages":1,"total_count":0,"page":1,"data":[]}`))
}

func TestNormalizeImdbID(t *testing.T) {
	cases := map[string]string{"tt0109424": "109424", "TT0000123": "123", "109424": "109424", "": ""}
	for in, want := range cases {
		if got := NormalizeImdbID(in); got != want {
			t.Errorf("NormalizeImdbID(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSearchSubtitlesByIMDBNormalizes(t *testing.T) {
	c, seen := newTestClient(t, okEmpty)
	if _, err := c.SearchSubtitlesByIMDB(context.Background(), "tt0109424"); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]; got != "/subtitles?imdb_id=109424" {
		t.Errorf("got %q", got)
	}
}

func TestSearchSubtitlesByEpisode(t *testing.T) {
	c, seen := newTestClient(t, okEmpty)
	if _, err := c.SearchSubtitlesByEpisode(context.Background(), "tt0903747", 1, 3); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]; got != "/subtitles?episode_number=3&parent_imdb_id=903747&season_number=1" {
		t.Errorf("got %q", got)
	}
}

func TestMoviehashMatchDecoded(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"1","attributes":{"language":"en","moviehash_match":true}}]}`))
	})
	subs, err := c.SearchSubtitlesByIMDB(context.Background(), "tt1")
	if err != nil || len(subs) != 1 || !subs[0].Attributes.MoviehashMatch {
		t.Fatalf("subs=%+v err=%v", subs, err)
	}
}

func TestValidImdbID(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"tt0109424", "109424", true},
		{"TT0000123", "123", true},
		{"109424", "109424", true},
		{" tt0109424 ", "109424", true},
		{"", "", false},
		{"tt", "", false},
		{"000", "", false},
		{"1&languages=xx", "", false},
		{"12a", "", false},
		{"tt12 34", "", false},
	}
	for _, c := range cases {
		got, ok := ValidImdbID(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ValidImdbID(%q)=(%q,%v) want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSearchSubtitlesByIMDBRejectsMalformed(t *testing.T) {
	for _, id := range []string{"", "tt", "000", "1&languages=xx"} {
		c, seen := newTestClient(t, okEmpty)
		if _, err := c.SearchSubtitlesByIMDB(context.Background(), id); err == nil {
			t.Errorf("SearchSubtitlesByIMDB(%q): want error", id)
		}
		if len(*seen) != 0 {
			t.Errorf("SearchSubtitlesByIMDB(%q): made requests %v", id, *seen)
		}
	}
}

func TestSearchSubtitlesByEpisodeRejectsMalformed(t *testing.T) {
	for _, id := range []string{"", "tt", "1&languages=xx"} {
		c, seen := newTestClient(t, okEmpty)
		if _, err := c.SearchSubtitlesByEpisode(context.Background(), id, 1, 2); err == nil {
			t.Errorf("SearchSubtitlesByEpisode(%q): want error", id)
		}
		if len(*seen) != 0 {
			t.Errorf("SearchSubtitlesByEpisode(%q): made requests %v", id, *seen)
		}
	}
}
