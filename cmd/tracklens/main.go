// Command tracklens summarises Toggl Track time entries and flags
// data-quality issues. Run it with -h for usage.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // so -tz works in minimal containers

	"github.com/c-dobkins/p1/internal/report"
	"github.com/c-dobkins/p1/internal/server"
	"github.com/c-dobkins/p1/internal/store"
	"github.com/c-dobkins/p1/internal/toggl"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// Exit codes.
const (
	exitOK       = 0
	exitError    = 1
	exitUsage    = 2
	exitFindings = 3 // -strict and at least one finding
)

const usageText = `tracklens summarises Toggl Track time entries and flags data-quality issues.

Usage:
  tracklens [flags]

Examples:
  tracklens -days 14                      # live API (needs TOGGL_API_TOKEN)
  tracklens -file testdata/sample.json    # offline sample
  tracklens -by day -tz Europe/Tallinn    # daily totals in a time zone
  tracklens -compare -format csv          # this period vs the previous one
  tracklens -days 30 -sync                # store in PostgreSQL (needs -db)
  tracklens -days 30 -sync -prune         # ...and drop entries deleted in Toggl
  tracklens -days 90 -history             # report from PostgreSQL
  tracklens -serve :8080 -history         # JSON/CSV API over PostgreSQL
  tracklens -serve :8080 -file testdata/sample.json

Environment:
  TOGGL_API_TOKEN          API token from your Toggl profile page
  TRACKLENS_DATABASE_URL   default for -db
  TOGGL_API_URL            override the API base URL (tests, proxies)

Exit status: 0 ok, 1 error, 2 bad usage, 3 findings with -strict.

Flags:
`

var errFindings = errors.New("data-quality findings present")

type config struct {
	days    int
	file    string
	format  report.Format
	byDay   bool
	tz      *time.Location
	dsn     string
	sync    bool
	prune   bool
	history bool
	strict  bool
	compare bool
	overlap time.Duration
	project string
	timeout time.Duration
	serve   string
	verbose bool
	version bool
}

// env reads environment variables; tests pass a map instead of os.Getenv.
type env func(string) string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv env, stdout, stderr io.Writer) int {
	c, err := parseFlags(args, getenv, stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return exitOK
	case err != nil:
		_, _ = fmt.Fprintf(stderr, "tracklens: %v\nRun 'tracklens -h' for usage.\n", err)
		return exitUsage
	}
	if c.version {
		_, _ = fmt.Fprintln(stdout, "tracklens", version)
		return exitOK
	}

	level := slog.LevelInfo
	if c.verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	if c.serve != "" {
		if err := runServer(ctx, c, log); err != nil {
			_, _ = fmt.Fprintf(stderr, "tracklens: %v\n", err)
			return exitError
		}
		return exitOK
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	switch err := runReport(ctx, c, getenv, log, stdout); {
	case errors.Is(err, errFindings):
		return exitFindings
	case err != nil:
		_, _ = fmt.Fprintf(stderr, "tracklens: %v\n", err)
		return exitError
	}
	return exitOK
}

func parseFlags(args []string, getenv env, stderr io.Writer) (config, error) {
	var c config
	var format, by, tz string
	fs := flag.NewFlagSet("tracklens", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = io.WriteString(stderr, usageText)
		fs.PrintDefaults()
	}
	fs.IntVar(&c.days, "days", 7, "number of days to report on")
	fs.StringVar(&c.file, "file", "", "read a saved API response (JSON) instead of calling the API")
	fs.StringVar(&format, "format", "table", "output format: table, json or csv")
	fs.StringVar(&by, "by", "project", "grouping: project or day")
	fs.StringVar(&tz, "tz", "UTC", "IANA time zone for -by day")
	fs.StringVar(&c.dsn, "db", getenv("TRACKLENS_DATABASE_URL"), "PostgreSQL connection string (default $TRACKLENS_DATABASE_URL)")
	fs.BoolVar(&c.sync, "sync", false, "store the fetched window in the database")
	fs.BoolVar(&c.prune, "prune", false, "with -sync, delete stored entries in the window that Toggl no longer returns")
	fs.BoolVar(&c.history, "history", false, "report from the database instead of the API")
	fs.BoolVar(&c.strict, "strict", false, "exit with status 3 if any data-quality finding is reported")
	fs.BoolVar(&c.compare, "compare", false, "compare the last -days with the -days before, by project")
	fs.DurationVar(&c.overlap, "overlap-tolerance", 0, "ignore overlaps up to this long, e.g. 2m")
	fs.StringVar(&c.project, "project", "", "only entries whose project name contains this text (case-insensitive)")
	fs.DurationVar(&c.timeout, "timeout", 2*time.Minute, "give up after this long (not used with -serve)")
	fs.StringVar(&c.serve, "serve", "", "serve the reports over HTTP on this address, e.g. :8080 (needs -history or -file)")
	fs.BoolVar(&c.verbose, "v", false, "log progress to stderr")
	fs.BoolVar(&c.version, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() > 0 {
		return c, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if c.serve != "" {
		var ignored []string
		fs.Visit(func(f *flag.Flag) {
			if slices.Contains(reportFlags, f.Name) {
				ignored = append(ignored, "-"+f.Name)
			}
		})
		if len(ignored) > 0 {
			return c, fmt.Errorf("-serve takes report options per request as query parameters; drop %s", strings.Join(ignored, " "))
		}
	}

	var err error
	if c.format, err = report.ParseFormat(format); err != nil {
		return c, fmt.Errorf("-format: %w", err)
	}
	switch by {
	case "project":
	case "day":
		c.byDay = true
	default:
		return c, fmt.Errorf("unknown -by %q (want project or day)", by)
	}
	if c.tz, err = time.LoadLocation(tz); err != nil {
		return c, fmt.Errorf("unknown -tz %q", tz)
	}
	return c, c.validate()
}

// reportFlags shape a single report; with -serve they come from each request.
var reportFlags = []string{"days", "format", "by", "tz", "strict", "compare", "overlap-tolerance", "project", "timeout"}

func (c config) validate() error {
	switch {
	case c.days < 1:
		return errors.New("-days must be at least 1")
	case c.timeout <= 0:
		return errors.New("-timeout must be positive")
	case c.overlap < 0:
		return errors.New("-overlap-tolerance cannot be negative")
	case c.compare && c.byDay:
		return errors.New("-compare reports by project; drop -by day")
	case c.prune && !c.sync:
		return errors.New("-prune only works with -sync")
	case c.sync && c.history:
		return errors.New("-sync and -history are mutually exclusive")
	case (c.sync || c.history) && c.file != "":
		return errors.New("-sync and -history cannot be combined with -file")
	case (c.sync || c.history) && c.dsn == "":
		return errors.New("-sync and -history need -db or $TRACKLENS_DATABASE_URL")
	case c.sync && c.project != "":
		return errors.New("-sync stores the whole window; drop -project")
	case c.serve != "" && c.sync:
		return errors.New("-serve is read-only; run -sync separately")
	case c.serve != "" && !c.history && c.file == "":
		return errors.New("-serve needs -history (with -db) or -file")
	}
	return nil
}

// runReport loads the data and writes the chosen report.
func runReport(ctx context.Context, c config, getenv env, log *slog.Logger, out io.Writer) error {
	d, err := load(ctx, c, getenv, log)
	if err != nil {
		return err
	}
	if c.project != "" {
		d.entries = report.FilterProject(d.entries, d.names, c.project)
		log.Debug("filtered", "project", c.project, "entries", len(d.entries))
	}

	switch {
	case c.compare:
		from, cutoff := report.Periods(d.now, c.days)
		return report.Render(out, c.format, report.Compare(d.entries, d.names, d.now, from, cutoff))
	case c.byDay:
		return report.Render(out, c.format, report.ByDay(d.entries, d.now, c.tz))
	}
	s := report.Summarize(d.entries, d.names, d.now, report.Options{OverlapTolerance: c.overlap})
	if err := report.Render(out, c.format, s); err != nil {
		return err
	}
	if c.strict && len(s.Findings) > 0 {
		return errFindings
	}
	return nil
}

// dataset is the input to every report.
type dataset struct {
	entries []toggl.Entry
	names   map[int64]string // project id -> name, may be partial
	now     time.Time        // sizes running timers and anchors the window
}

// load reads the report window from a fixture, the database or the API,
// syncing to the database when asked.
func load(ctx context.Context, c config, getenv env, log *slog.Logger) (dataset, error) {
	d := dataset{now: time.Now().UTC()}
	var src server.Source
	switch {
	case c.file != "":
		log.Debug("source", "kind", "file", "path", c.file)
		f, err := newFixtureSource(c.file)
		if err != nil {
			return d, err
		}
		if f.now != nil {
			d.now = *f.now
		}
		src = f
	case c.history:
		log.Debug("source", "kind", "history")
		st, err := store.OpenReader(ctx, c.dsn)
		if err != nil {
			return d, err
		}
		defer func() { _ = st.Close() }()
		src = st
	}

	from, to := report.Window(d.now, c.days, c.compare)
	log.Debug("window", "from", from, "to", to)

	if src == nil { // the live API
		entries, err := loadAPI(ctx, getenv, from, to)
		if err != nil {
			return d, err
		}
		d.entries, d.names = entries, toggl.ProjectNames(entries)
		if c.sync {
			err = syncStore(ctx, c.dsn, from, to, entries, store.SyncOptions{Prune: c.prune}, log)
		}
		return d, err
	}

	var err error
	if d.entries, err = src.Entries(ctx, from, to); err != nil {
		return d, err
	}
	if d.names, err = src.Projects(ctx); err != nil {
		return d, err
	}
	if f, ok := src.(*fixtureSource); ok && len(d.entries) == 0 && len(f.entries) > 0 {
		log.Warn("no fixture entries fall in the window; set \"now\" in the file or raise -days",
			"entries", len(f.entries), "from", from.Format(time.RFC3339))
	}
	return d, nil
}

func loadAPI(ctx context.Context, getenv env, from, to time.Time) ([]toggl.Entry, error) {
	token := getenv("TOGGL_API_TOKEN")
	if token == "" {
		return nil, errors.New("set TOGGL_API_TOKEN (from your Toggl profile page), or use -file or -history")
	}
	api := toggl.New(token)
	api.UserAgent = "tracklens/" + version + " (+https://github.com/c-dobkins/p1)"
	if u := getenv("TOGGL_API_URL"); u != "" {
		api.BaseURL = u
	}
	return api.Entries(ctx, from, to)
}

func syncStore(ctx context.Context, dsn string, from, to time.Time, entries []toggl.Entry, opts store.SyncOptions, log *slog.Logger) error {
	st, err := store.Open(ctx, dsn) // a writer: applies pending migrations
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	res, err := st.Sync(ctx, from, to, entries, opts)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	log.Info("synced", "entries", res.Upserted, "removed", res.Deleted,
		"from", from.Format(time.RFC3339), "to", to.Format(time.RFC3339))
	return nil
}

// fixture is a saved /me/time_entries?meta=true response. now pins the clock
// so output is reproducible; it defaults to the current time.
type fixture struct {
	Now     *time.Time    `json:"now"`
	Entries []toggl.Entry `json:"entries"`
}

// fixtureSource serves a fixture through the same interface as the store.
type fixtureSource struct {
	entries []toggl.Entry
	names   map[int64]string // computed once; the file never changes
	now     *time.Time
}

func newFixtureSource(path string) (*fixtureSource, error) {
	f, err := readFixture(path)
	if err != nil {
		return nil, err
	}
	return &fixtureSource{entries: f.Entries, names: toggl.ProjectNames(f.Entries), now: f.Now}, nil
}

func (f *fixtureSource) Entries(_ context.Context, from, to time.Time) ([]toggl.Entry, error) {
	return inWindow(f.entries, from, to), nil
}

func (f *fixtureSource) Projects(context.Context) (map[int64]string, error) { return f.names, nil }

func (*fixtureSource) Ping(context.Context) error { return nil }

// startupTimeout bounds connecting to the database before serving, so an
// unreachable database fails the start instead of hanging it.
const startupTimeout = 30 * time.Second

// runServer serves the API until ctx is cancelled, then shuts down
// gracefully, letting in-flight requests finish.
func runServer(ctx context.Context, c config, log *slog.Logger) error {
	var src server.Source
	now := func() time.Time { return time.Now().UTC() }
	if c.file != "" {
		f, err := newFixtureSource(c.file)
		if err != nil {
			return err
		}
		if f.now != nil {
			fixed := *f.now
			now = func() time.Time { return fixed }
		}
		src = f
	} else {
		startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
		st, err := store.OpenReader(startCtx, c.dsn) // never migrates; works with a read-only role
		cancel()
		if err != nil {
			return err
		}
		defer func() { _ = st.Close() }()
		src = st
	}

	srv := &http.Server{
		Handler:           server.New(src, log, now),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      server.RequestTimeout + 10*time.Second,
		IdleTimeout:       time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", c.serve)
	if err != nil {
		return err
	}
	log.Info("listening", "addr", ln.Addr().String())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func readFixture(path string) (fixture, error) {
	var f fixture
	b, err := os.ReadFile(path) //nolint:gosec // reading the file the user named is the point
	if err != nil {
		return f, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // an outdated or mistyped fixture fails loudly
	if err := dec.Decode(&f); err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// inWindow keeps entries that started in [from, to).
func inWindow(es []toggl.Entry, from, to time.Time) []toggl.Entry {
	var out []toggl.Entry
	for _, e := range es {
		if !e.Start.Before(from) && e.Start.Before(to) {
			out = append(out, e)
		}
	}
	return out
}
