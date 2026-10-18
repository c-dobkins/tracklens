package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/report"
	"github.com/c-dobkins/p1/internal/toggl"
)

var now = time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)

// fakeSource records the window it was asked for.
type fakeSource struct {
	entries  []toggl.Entry
	err      error
	from, to time.Time
}

func (f *fakeSource) Entries(_ context.Context, from, to time.Time) ([]toggl.Entry, error) {
	f.from, f.to = from, to
	return f.entries, f.err
}

func (f *fakeSource) Projects(context.Context) (map[int64]string, error) {
	return map[int64]string{1: "Client portal", 2: "Internal tooling"}, f.err
}

func (f *fakeSource) Ping(context.Context) error { return f.err }

func sample() []toggl.Entry {
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	return []toggl.Entry{
		{ID: 1, ProjectID: new(int64(1)), Billable: true, Start: at(15, 9), Stop: new(at(15, 11)), Duration: 7200},
		{ID: 2, ProjectID: new(int64(2)), Start: at(16, 9), Stop: new(at(16, 10)), Duration: 3600},
		{ID: 3, Start: at(16, 17), Duration: -1},
	}
}

func get(t *testing.T, src Source, target string) *httptest.ResponseRecorder {
	t.Helper()
	h := New(src, slog.New(slog.DiscardHandler), func() time.Time { return now })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
	return rec
}

func TestSummaryJSON(t *testing.T) {
	rec := get(t, &fakeSource{entries: sample()}, "/v1/summary")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var body struct {
		Hours float64 `json:"total_hours"`
		Rows  []struct {
			Project string `json:"project"`
		} `json:"rows"`
		Findings []struct {
			Kind string `json:"kind"`
		} `json:"findings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Hours != 4 || len(body.Rows) != 3 || body.Rows[0].Project != "Client portal" || len(body.Findings) != 2 {
		t.Fatalf("body = %+v", body)
	}
}

func TestEndpointsAndFormats(t *testing.T) {
	for _, path := range []string{"/v1/summary", "/v1/days", "/v1/compare"} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, &fakeSource{entries: sample()}, path+"?format=csv")
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
				t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
			}
			if recs, err := csv.NewReader(rec.Body).ReadAll(); err != nil || len(recs) < 2 {
				t.Fatalf("csv %v: %v", recs, err)
			}
			if rec := get(t, &fakeSource{entries: sample()}, path); !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("invalid JSON: %s", rec.Body.String())
			}
		})
	}
}

func TestWindowAndFilters(t *testing.T) {
	src := &fakeSource{entries: sample()}
	get(t, src, "/v1/summary?days=3")
	if !src.from.Equal(now.AddDate(0, 0, -3)) || !src.to.Equal(now.Add(toggl.ClockSkew)) {
		t.Fatalf("summary window %v..%v", src.from, src.to)
	}
	get(t, src, "/v1/compare?days=3") // needs the previous period too, plus a margin
	if from, _ := report.Window(now, 3, true); !src.from.Equal(from) {
		t.Fatalf("compare window starts %v", src.from)
	}

	rec := get(t, src, "/v1/summary?project=PORTAL&format=csv")
	if want := "project,hours,billable_hours\nClient portal,2.00,2.00\n"; rec.Body.String() != want {
		t.Fatalf("filtered csv = %q", rec.Body.String())
	}

	rec = get(t, src, "/v1/days?tz=Asia/Tokyo&format=csv")
	if !strings.Contains(rec.Body.String(), "2026-10-17") { // 17:00 UTC on the 16th is the 17th in Tokyo
		t.Fatalf("tz not applied: %s", rec.Body.String())
	}
}

func TestBadParameters(t *testing.T) {
	for _, q := range []string{
		"summary?days=0", "summary?days=367", "summary?days=x", "summary?format=xml", "summary?format=table",
		"days?tz=Mars/Base", "summary?overlap_tolerance=-1m", "summary?overlap_tolerance=soon",
		"summary?tz=UTC", "compare?overlap_tolerance=1m", "days?overlap_tolerance=1m", "summary?dayz=3",
	} {
		t.Run(q, func(t *testing.T) {
			rec := get(t, &fakeSource{}, "/v1/"+q)
			var body struct {
				Error string `json:"error"`
			}
			if rec.Code != http.StatusBadRequest || json.NewDecoder(rec.Body).Decode(&body) != nil || body.Error == "" {
				t.Fatalf("status %d, body %+v", rec.Code, body)
			}
		})
	}
}

func TestSourceFailureIs503WithoutLeakingDetails(t *testing.T) {
	src := &fakeSource{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	for _, path := range []string{"/v1/summary", "/healthz"} {
		rec := get(t, src, path)
		if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "10.0.0.5") {
			t.Fatalf("%s: status %d, body %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestHealthAndRouting(t *testing.T) {
	if rec := get(t, &fakeSource{}, "/healthz"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("health: %d %s", rec.Code, rec.Body.String())
	}
	if rec := get(t, &fakeSource{}, "/v1/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: %d", rec.Code)
	}

	h := New(&fakeSource{}, slog.New(slog.DiscardHandler), func() time.Time { return now })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/summary", strings.NewReader("{}")))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
}

func TestRequestsAreLogged(t *testing.T) {
	var logs strings.Builder
	h := New(&fakeSource{}, slog.New(slog.NewTextHandler(&logs, nil)), func() time.Time { return now })
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/summary?days=x", http.NoBody))
	if !strings.Contains(logs.String(), "path=/v1/summary") || !strings.Contains(logs.String(), "status=400") {
		t.Fatalf("logs: %s", logs.String())
	}
}

func TestClientGoneDuringWriteIsLogged(t *testing.T) {
	var logs strings.Builder
	h := New(&fakeSource{entries: sample()}, slog.New(slog.NewTextHandler(&logs, nil)), func() time.Time { return now })
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/summary", http.NoBody)
	h.ServeHTTP(brokenWriter{httptest.NewRecorder()}, req)
	if !strings.Contains(logs.String(), "write response") {
		t.Fatalf("logs: %s", logs.String())
	}
}

type brokenWriter struct{ *httptest.ResponseRecorder }

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// slowSource blocks until the request's context ends.
type slowSource struct{ fakeSource }

func (slowSource) Entries(ctx context.Context, _, _ time.Time) ([]toggl.Entry, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRequestContextEndsTheQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	h := New(&slowSource{}, slog.New(slog.DiscardHandler), func() time.Time { return now })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/summary", http.NoBody))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}
