package toggl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestEntriesAuthAndDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != "tok" || p != "api_token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("start_date") != "2026-10-01" {
			t.Errorf("start_date = %q", r.URL.Query().Get("start_date"))
		}
		w.Write([]byte(`[{"id":7,"project_id":null,"billable":true,"start":"2026-10-01T09:00:00Z","stop":null,"duration":-1}]`))
	}))
	defer srv.Close()

	c := New("tok")
	c.BaseURL = srv.URL
	es, err := c.Entries(context.Background(), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 || es[0].ID != 7 || es[0].ProjectID != nil || es[0].Stop != nil {
		t.Fatalf("unexpected entries: %+v", es)
	}

	c.Token = "bad"
	if _, err := c.Entries(context.Background(), time.Now(), time.Now()); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestRetriesOnRateLimit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New("tok")
	c.BaseURL, c.backoff = srv.URL, time.Millisecond
	if _, err := c.Entries(context.Background(), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := New("bad")
	c.BaseURL, c.backoff = srv.URL, time.Millisecond
	if _, err := c.Entries(context.Background(), time.Now(), time.Now()); err == nil || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}
