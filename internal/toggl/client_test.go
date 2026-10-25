package toggl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.AddDate(0, 0, 7)
)

// newTestClient points a client at h with retries that do not sleep.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("tok")
	c.BaseURL, c.backoff = srv.URL, time.Millisecond
	return c
}

func TestEntriesRequestShape(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "tok" || p != "api_token" {
			t.Errorf("basic auth = %q/%q", u, p)
		}
		q := r.URL.Query()
		if q.Get("start_date") != "2026-10-01T00:00:00Z" || q.Get("end_date") != "2026-10-08T00:00:00Z" || q.Get("meta") != "true" {
			t.Errorf("query = %v", q)
		}
		if ua := r.Header.Get("User-Agent"); ua != "tracklens" {
			t.Errorf("User-Agent = %q", ua)
		}
		_, _ = w.Write([]byte(`[{"id":7,"workspace_id":3,"project_id":5,"project_name":"Portal","billable":true,
			"start":"2026-10-01T09:00:00Z","stop":null,"duration":-1}]`))
	})

	es, err := c.Entries(context.Background(), t0, t1)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 || es[0].WorkspaceID != 3 || *es[0].ProjectID != 5 || !es[0].Running() || es[0].Stop != nil {
		t.Fatalf("unexpected entries: %+v", es)
	}
	if names := ProjectNames(es); names[5] != "Portal" {
		t.Fatalf("ProjectNames = %v", names)
	}
}

func TestElapsed(t *testing.T) {
	start := t0.Add(9 * time.Hour)
	closed := Entry{Start: start, Duration: 90}
	running := Entry{Start: start, Duration: -1}
	if closed.Elapsed(start) != 90*time.Second || !closed.End(start).Equal(start.Add(90*time.Second)) {
		t.Errorf("closed: %v", closed.Elapsed(start))
	}
	if running.Elapsed(start.Add(time.Hour)) != time.Hour {
		t.Errorf("running: %v", running.Elapsed(start.Add(time.Hour)))
	}
	if running.Elapsed(start.Add(-time.Hour)) != 0 { // clock skew must not go negative
		t.Errorf("skewed: %v", running.Elapsed(start.Add(-time.Hour)))
	}
}

// fakeEntries serves entries filtered by the requested window. Both bounds are
// inclusive, which is the worst case for splitting.
func fakeEntries(t *testing.T, all []Entry, calls *int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("start_date"))
		to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("end_date"))
		if err1 != nil || err2 != nil {
			t.Errorf("bad window: %v %v", err1, err2)
		}
		var out []Entry
		for _, e := range all {
			if !e.Start.Before(from) && !e.Start.After(to) {
				out = append(out, e)
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func TestEntriesSplitsAtCap(t *testing.T) {
	var all []Entry
	for i := range 9 {
		all = append(all, Entry{ID: int64(i + 1), Start: t0.Add(time.Duration(i) * 17 * time.Hour), Duration: 60})
	}
	all = append(all, Entry{ID: 99, Start: t0.Add(84 * time.Hour), Duration: 60}) // exactly the first split point

	var calls int32
	c := newTestClient(t, fakeEntries(t, all, &calls))
	c.pageLimit = 3

	got, err := c.Entries(context.Background(), t0, t1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(all) {
		t.Fatalf("got %d entries, want %d (calls %d)", len(got), len(all), calls)
	}
	seen := map[int64]bool{}
	for _, e := range got {
		if seen[e.ID] {
			t.Fatalf("duplicate entry %d", e.ID)
		}
		seen[e.ID] = true
	}
	if calls < 3 {
		t.Fatalf("expected the window to be split, got %d calls", calls)
	}
}

func TestEntriesCannotSplitFurther(t *testing.T) {
	same := []Entry{{ID: 1, Start: t0}, {ID: 2, Start: t0}, {ID: 3, Start: t0}}
	var calls int32
	c := newTestClient(t, fakeEntries(t, same, &calls))
	c.pageLimit = 2
	_, err := c.Entries(context.Background(), t0, t0.Add(time.Second))
	if err == nil || !strings.Contains(err.Error(), "cannot split further") {
		t.Fatalf("err = %v", err)
	}
}

func TestRetries(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		header    string
		failFirst int32 // number of failing responses before success
		wantCalls int32
		wantErr   bool
	}{
		{"429 then ok", http.StatusTooManyRequests, "", 2, 3, false},
		{"5xx then ok", http.StatusBadGateway, "", 1, 2, false},
		{"Retry-After honoured", http.StatusTooManyRequests, "0", 1, 2, false},
		{"gives up after max attempts", http.StatusServiceUnavailable, "", 99, maxAttempts, true},
		{"403 is final", http.StatusForbidden, "", 99, 1, true},
		{"402 quota is final", http.StatusPaymentRequired, "", 99, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				if atomic.AddInt32(&calls, 1) <= tc.failFirst {
					if tc.header != "" {
						w.Header().Set("Retry-After", tc.header)
					}
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			})
			if tc.header != "" {
				c.backoff = time.Hour // only the header can make this pass quickly
			}
			_, err := c.Entries(context.Background(), t0, t1)
			if (err != nil) != tc.wantErr || calls != tc.wantCalls {
				t.Fatalf("err = %v, calls = %d; want err %v, calls %d", err, calls, tc.wantErr, tc.wantCalls)
			}
		})
	}
}

func TestAPIErrorCarriesStatusBodyAndHint(t *testing.T) {
	for status, hint := range map[int]string{
		http.StatusForbidden:       "check TOGGL_API_TOKEN",
		http.StatusPaymentRequired: "quota",
		http.StatusBadRequest:      "",
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "server says no", status)
			})
			_, err := c.Entries(context.Background(), t0, t1)
			apiErr, ok := errors.AsType[*APIError](err)
			if !ok || apiErr.StatusCode != status || apiErr.Message != "server says no" {
				t.Fatalf("err = %#v", err)
			}
			if !strings.Contains(err.Error(), hint) {
				t.Fatalf("missing hint %q in %q", hint, err)
			}
		})
	}
}

func TestMalformedBodyIsNotRetried(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`not json`))
	})
	if _, err := c.Entries(context.Background(), t0, t1); err == nil || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestTransportErrorIsRetried(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close() // drop the connection mid-request
			}
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})
	if _, err := c.Entries(context.Background(), t0, t1); err != nil || calls != 2 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestContextCancelStopsRetries(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.backoff = 30 * time.Second // under the cap, so the client really waits
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := c.Entries(ctx, t0, t1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestLongRetryAfterFailsFast(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.Entries(context.Background(), t0, t1)
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests || !strings.Contains(err.Error(), "1h0m0s") || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestRetryWaitBeyondDeadlineFailsFast(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := c.Entries(ctx, t0, t1); err == nil || !strings.Contains(err.Error(), "503") || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}

func TestPermanentTransportErrorsAreNotRetried(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(tlsSrv.Close)
	for name, base := range map[string]string{
		"no scheme":         "api.example.invalid",
		"untrusted TLS":     tlsSrv.URL,
		"unsupported proto": "ftp://example.invalid",
	} {
		t.Run(name, func(t *testing.T) {
			c := New("tok")
			c.BaseURL, c.backoff = base, time.Hour // a retry would hang the test
			start := time.Now()
			if _, err := c.Entries(context.Background(), t0, t1); err == nil || time.Since(start) > 5*time.Second {
				t.Fatalf("err = %v after %v", err, time.Since(start))
			}
		})
	}
}

func TestZeroValueClientUsesDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "tracklens" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`[{"id":1},{"id":2}]`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "tok"} // no HTTP, page limit or backoff
	got, err := c.Entries(context.Background(), t0, t1)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDuplicateIDsInOneResponseAreDropped(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1},{"id":2},{"id":1}]`))
	})
	got, err := c.Entries(context.Background(), t0, t1)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
}
