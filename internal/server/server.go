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
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/c-dobkins/tracklens/internal/report"
	"github.com/c-dobkins/tracklens/internal/toggl"
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
// ui, when not nil, is the built dashboard, served at /.
func New(src Source, log *slog.Logger, now func() time.Time, ui fs.FS) http.Handler {
	s := &Server{src: src, log: log, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/summary", s.summary)
	mux.HandleFunc("GET /v1/days", s.days)
	mux.HandleFunc("GET /v1/compare", s.compare)
	mux.HandleFunc("GET /healthz", s.health)
	if ui != nil {
		mux.Handle("GET /", static(http.FileServerFS(ui)))
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "tracklens API: see /v1/summary, /v1/days, /v1/compare and /healthz.\n"+
				"This build has no dashboard; build it with `npm --prefix web run build` and `go build -tags webui`.\n")
		})
	}
	return s.logRequests(securityHeaders(mux))
}

// contentSecurityPolicy allows only same-origin scripts and requests. Inline
// styles are allowed because the chart library positions tooltips with them.
const contentSecurityPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// static serves the dashboard's files. Vite fingerprints everything under
// /assets/, so those can be cached forever; index.html must always revalidate.
func static(files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only files are served: no directory listings (the root serves index.html).
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
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

// loaded is everything a report endpoint builds from.
type loaded struct {
	params
	entries  []toggl.Entry
	names    map[int64]string
	now      time.Time
	from, to time.Time // the start-time range that was loaded
}

// load fetches the entries in the endpoint's window, filtered by project.
func (s *Server) load(ctx context.Context, p params, e endpoint) (loaded, error) {
	d := loaded{params: p, now: s.now()}
	d.from, d.to = e.window(d.now, p)
	var err error
	if d.entries, err = s.src.Entries(ctx, d.from, d.to); err != nil {
		return d, err
	}
	if d.names, err = s.src.Projects(ctx); err != nil {
		return d, err
	}
	if p.project != "" {
		d.entries = report.FilterProject(d.entries, d.names, p.project)
	}
	return d, nil
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, endpoint{
		params: []string{"overlap_tolerance"},
		window: func(now time.Time, p params) (time.Time, time.Time) { return report.Window(now, p.days, false) },
		build: func(d loaded) report.Renderer {
			return report.Summarize(d.entries, d.names, d.now, report.Options{OverlapTolerance: d.overlap})
		},
	})
}

func (s *Server) days(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, endpoint{
		params: []string{"tz"},
		window: func(now time.Time, p params) (time.Time, time.Time) { return report.DayWindow(now, p.days, p.tz) },
		build: func(d loaded) report.Renderer {
			// Charts need every day of the period, including empty ones.
			return report.ByDay(d.entries, d.now, d.tz).Fill(d.from, d.now, d.tz)
		},
	})
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, endpoint{
		window: func(now time.Time, p params) (time.Time, time.Time) { return report.Window(now, p.days, true) },
		build: func(d loaded) report.Renderer {
			from, cutoff := report.Periods(d.now, d.days)
			return report.Compare(d.entries, d.names, d.now, from, cutoff)
		},
	})
}

// endpoint describes one report endpoint.
type endpoint struct {
	params []string // accepted in addition to commonParams
	window func(now time.Time, p params) (from, to time.Time)
	build  func(loaded) report.Renderer
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
	d, err := s.load(ctx, p, e)
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
	if err := report.Render(w, p.format, e.build(d)); err != nil {
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
