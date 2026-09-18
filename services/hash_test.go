package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type memHashCache struct {
	hash uint64
	size int64
}

func (m *memHashCache) GetHashAndSize(context.Context) (uint64, int64, error) {
	return m.hash, m.size, nil
}
func (m *memHashCache) SetHashAndSize(_ context.Context, h uint64, s int64) error {
	m.hash, m.size = h, s
	return nil
}

// TestHashReadStopsWithTheCaller: seekinghttp builds requests without a
// context, so a viewer who navigated away used to keep the seeder read
// going for the full five-minute client timeout — and the log called it
// hash_timeout. With the context reattached the read stops with the
// request and the failure reads as client_gone.
func TestHashReadStopsWithTheCaller(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	t.Cleanup(func() { close(blocked); srv.Close() })

	h := NewHash(srv.URL, &memHashCache{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := h.Get(ctx, false)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the failure must read as the caller leaving: %v", err)
		}
		if failureReason("hash", err) != reasonClientGone {
			t.Fatalf("reason: %q", failureReason("hash", err))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not stop with the caller")
	}
}
