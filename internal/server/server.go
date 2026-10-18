// Package server exposes the reports over a small read-only HTTP API.
//
//	GET /v1/summary?days=7&project=&overlap_tolerance=0s   per-project totals and findings
//	GET /v1/days?days=7&tz=UTC&project=                     per-day totals
//	GET /v1/compare?days=7&project=                         this period vs the previous one
//	GET /healthz                                            liveness and data-source check
//
// Every report endpoint answers JSON by default and CSV with format=csv. A
// parameter an endpoint does not use is rejected rather than ignored.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/c-dobkins/p1/internal/report"
	"github.com/c-dobkins/p1/internal/toggl"
)

// MaxDays bounds the window a single request may ask for.
const MaxDays = 366

// RequestTimeout bounds the work for one request, below a typical
// http.Server WriteTimeout so a slow query ends in a 503, not a dropped
// connection.
const RequestTimeout = 20 * time.Second

// Source supplies the entries the API reports on.
type Source interface {
	Entries(ctx context.Context, from, to time.Time) ([]toggl.Entry, error)
	Projects(ctx context.Context) (map[int64]string, error)
	Ping(ctx context.Context) error
}

// Server handles API requests. Create one with New.
type Server struct {
	src   Source
	log   *slog.Logger
	now   func() time.Time
	zones sync.Map // tz name -> *time.Location
}

// New returns the API handler. now is the report clock, usually time.Now.
func New(src Source, log *slog.Logger, now func() time.Time) http.Handler {
	s := &Server{src: src, log: log, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/summary", s.summary)
	mux.HandleFunc("GET /v1/days", s.days)
	mux.HandleFunc("GET /v1/compare", s.compare)
	mux.HandleFunc("GET /healthz", s.health)
	return s.logRequests(mux)
}

// params are the query parameters shared by the report endpoints.
type params struct {
	days    int
	project string
	format  report.Format
	tz      *time.Location
	overlap time.Duration
}

// Query parameters every report endpoint accepts.
var commonParams = []string{"days", "project", "format"}

func (s *Server) parseParams(r *http.Request, extra ...string) (params, error) {
	q := r.URL.Query()
	for key := range q {
		if !slices.Contains(commonParams, key) && !slices.Contains(extra, key) {
			return params{}, fmt.Errorf("unknown parameter %q for %s", key, r.URL.Path)
		}
	}
	p := params{days: 7, project: q.Get("project"), format: report.FormatJSON, tz: time.UTC}
	if v := q.Get("days"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 1 || d > MaxDays {
			return p, fmt.Errorf("days must be a whole number from 1 to %d", MaxDays)
		}
		p.days = d
	}
	if v := q.Get("format"); v != "" {
		f, err := report.ParseFormat(v)
		if err != nil || f == report.FormatTable {
			return p, errors.New("format must be json or csv")
		}
		p.format = f
	}
	if v := q.Get("tz"); v != "" {
		loc, err := s.location(v)
		if err != nil {
			return p, fmt.Errorf("unknown tz %q", v)
		}
		p.tz = loc
	}
	if v := q.Get("overlap_tolerance"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return p, errors.New("overlap_tolerance must be a non-negative duration such as 2m")
		}
		p.overlap = d
	}
	return p, nil
}

// location caches time.LoadLocation, which reads zoneinfo on every call.
func (s *Server) location(name string) (*time.Location, error) {
	if loc, ok := s.zones.Load(name); ok {
		return loc.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	s.zones.Store(name, loc)
	return loc, nil
}

// load fetches the entries for the report window, filtered by project.
func (s *Server) load(ctx context.Context, p params, compare bool) ([]toggl.Entry, map[int64]string, time.Time, error) {
	now := s.now()
	from, to := report.Window(now, p.days, compare)
	entries, err := s.src.Entries(ctx, from, to)
	if err != nil {
		return nil, nil, now, err
	}
	names, err := s.src.Projects(ctx)
	if err != nil {
		return nil, nil, now, err
	}
	if p.project != "" {
		entries = report.FilterProject(entries, names, p.project)
	}
	return entries, names, now, nil
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, endpoint{params: []string{"overlap_tolerance"},
		build: func(p params, es []toggl.Entry, names map[int64]string, now time.Time) report.Renderer {
			return report.Summarize(es, names, now, report.Options{OverlapTolerance: p.overlap})
		}})
}

func (s *Server) days(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, endpoint{params: []string{"tz"},
		build: func(p params, es []toggl.Entry, _ map[int64]string, now time.Time) report.Renderer {
			return report.ByDay(es, now, p.tz)
		}})
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, endpoint{compare: true,
		build: func(p params, es []toggl.Entry, names map[int64]string, now time.Time) report.Renderer {
			from, cutoff := report.Periods(now, p.days)
			return report.Compare(es, names, now, from, cutoff)
		}})
}

// endpoint describes one report endpoint.
type endpoint struct {
	params  []string // accepted in addition to commonParams
	compare bool     // load the previous period too
	build   func(params, []toggl.Entry, map[int64]string, time.Time) report.Renderer
}

// serve parses the request, loads the window, builds the report and writes
// it in the requested format.
func (s *Server) serve(w http.ResponseWriter, r *http.Request, e endpoint) {
	p, err := s.parseParams(r, e.params...)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
	defer cancel()
	entries, names, now, err := s.load(ctx, p, e.compare)
	if err != nil {
		s.log.ErrorContext(r.Context(), "load entries", "err", err)
		writeError(w, http.StatusServiceUnavailable, "data source unavailable")
		return
	}

	contentType := "application/json"
	if p.format == report.FormatCSV {
		contentType = "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	if err := report.Render(w, p.format, e.build(p, entries, names, now)); err != nil {
		s.log.WarnContext(r.Context(), "write response", "err", err) // the client has most likely gone
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.src.Ping(ctx); err != nil {
		s.log.ErrorContext(r.Context(), "health check", "err", err)
		writeError(w, http.StatusServiceUnavailable, "data source unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // nothing useful to do if the client has gone
}

// statusRecorder captures the status code for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.InfoContext(r.Context(), "request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration", time.Since(start).Round(time.Microsecond))
	})
}
