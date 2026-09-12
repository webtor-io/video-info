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
