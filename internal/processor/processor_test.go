package processor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestURLProcessor_Basic(t *testing.T) {
	// Local test server - no network dependency
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Hello from Concurro Test</title></head><body>OK</body></html>`))
	}))
	defer ts.Close()

	p := NewURLProcessor()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := p.Process(ctx, ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
	if res.Title != "Hello from Concurro Test" {
		t.Errorf("expected title extracted, got %q", res.Title)
	}
	if res.ContentLength == 0 {
		t.Error("expected non-zero content length")
	}
}

func TestURLProcessor_Cancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte("slow"))
	}))
	defer ts.Close()

	p := NewURLProcessor()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	_, err := p.Process(ctx, ts.URL)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
