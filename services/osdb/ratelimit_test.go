package osdb

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPadMoviehash(t *testing.T) {
	cases := map[string]string{
		"239f938f5b1d6ebd": "239f938f5b1d6ebd",
		"39f938f5b1d6ebd":  "039f938f5b1d6ebd",
		"9f938f5b1d6ebd":   "009f938f5b1d6ebd", // two leading zero nibbles: the old loop stopped at 15 chars
		"1":                "0000000000000001",
	}
	for in, want := range cases {
		if got := padMoviehash(in); got != want {
			t.Errorf("padMoviehash(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSearchSubtitlesByHashPadsTo16(t *testing.T) {
	c, seen := newTestClient(t, okEmpty)
	if _, err := c.SearchSubtitlesByHash(context.Background(), "9f938f5b1d6ebd"); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]; got != "/subtitles?moviehash=009f938f5b1d6ebd" {
		t.Errorf("got %q", got)
	}
}

func TestSearchRetriesOn429(t *testing.T) {
	var n int32
	c, seen := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) <= 2 {
			w.Header().Set("ratelimit-reset", "0")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		okEmpty(w, r)
	})
	c.retryBackoff = 5 * time.Millisecond
	if _, err := c.SearchSubtitlesByIMDB(context.Background(), "tt1"); err != nil {
		t.Fatalf("expected success after two 429s, got %v", err)
	}
	if len(*seen) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(*seen))
	}
}

func TestSearchGivesUpAfterMaxRetries(t *testing.T) {
	c, seen := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	})
	c.retryBackoff = time.Millisecond
	_, err := c.SearchSubtitlesByIMDB(context.Background(), "tt1")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected a 429 error, got %v", err)
	}
	if len(*seen) != maxAttempts {
		t.Fatalf("expected %d attempts, got %d", maxAttempts, len(*seen))
	}
}

func TestRetryStopsOnCancelledContext(t *testing.T) {
	c, seen := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	})
	c.retryBackoff = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := c.SearchSubtitlesByIMDB(ctx, "tt1")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(*seen) != 1 {
		t.Fatalf("expected the retry to stop on cancel after 1 attempt, got %d", len(*seen))
	}
}

func TestDownloadRetriesOn429(t *testing.T) {
	var n int32
	var srvURL string
	c, seen := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"token":"T","status":200}`))
		case "/download":
			if atomic.AddInt32(&n, 1) == 1 {
				w.WriteHeader(429)
				return
			}
			_, _ = w.Write([]byte(`{"link":"` + srvURL + `/file.vtt","file_name":"f.vtt"}`))
		case "/file.vtt":
			if atomic.AddInt32(&n, 1) == 3 {
				w.WriteHeader(429)
				return
			}
			_, _ = w.Write([]byte("WEBVTT\n"))
		default:
			w.WriteHeader(404)
		}
	})
	srvURL = c.apiURL
	c.retryBackoff = time.Millisecond
	d, err := c.DownloadSubtitle(context.Background(), 1, "webvtt")
	if err != nil || string(d) != "WEBVTT\n" {
		t.Fatalf("d=%q err=%v", d, err)
	}
	// login, download(429), download, link(429), link
	if len(*seen) != 5 {
		t.Fatalf("expected 5 requests, got %d: %v", len(*seen), *seen)
	}
}

func TestLimiterPacesRequests(t *testing.T) {
	c, _ := newTestClient(t, okEmpty)
	c.SetRate(20) // 20 req/s => 50 ms apart after the first burst token
	start := time.Now()
	for i := 0; i < 4; i++ {
		if _, err := c.SearchSubtitlesByIMDB(context.Background(), "tt1"); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 100*time.Millisecond {
		t.Fatalf("4 requests at 20 rps with burst 1 should take >= 150ms, took %v", el)
	}
}
