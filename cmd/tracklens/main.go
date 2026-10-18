// tracklens summarises Toggl Track time entries and flags data-quality issues.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // so -tz works in minimal containers

	"github.com/c-dobkins/p1/internal/report"
	"github.com/c-dobkins/p1/internal/store"
	"github.com/c-dobkins/p1/internal/toggl"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// errFindings makes -strict exit non-zero without printing an error.
var errFindings = errors.New("data-quality findings present")

type config struct {
	days    int
	file    string
	format  string
	by      string
	tz      *time.Location
	dsn     string
	sync    bool
	history bool
	strict  bool
	compare bool
	overlap time.Duration
	log     *slog.Logger // nil means silent
}

type fixture struct {
	Projects []toggl.Project `json:"projects"`
	Entries  []toggl.Entry   `json:"entries"`
}

func main() {
	var c config
	var tz string
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.IntVar(&c.days, "days", 7, "number of days to look back")
	flag.StringVar(&c.file, "file", "", "read entries from a JSON fixture instead of the API")
	flag.StringVar(&c.format, "format", "table", "output format: table, json or csv")
	flag.StringVar(&c.by, "by", "project", "grouping: project or day")
	flag.StringVar(&tz, "tz", "UTC", "IANA time zone used for -by day")
	flag.StringVar(&c.dsn, "db", os.Getenv("TRACKLENS_DATABASE_URL"), "PostgreSQL connection string (default $TRACKLENS_DATABASE_URL)")
	flag.BoolVar(&c.sync, "sync", false, "save fetched entries to the database")
	flag.BoolVar(&c.history, "history", false, "read entries from the database instead of the API")
	flag.BoolVar(&c.strict, "strict", false, "exit with status 2 if any data-quality finding is reported")
	flag.BoolVar(&c.compare, "compare", false, "compare the last -days against the -days before that, by project")
	flag.DurationVar(&c.overlap, "overlap-tolerance", 0, "ignore overlapping entries by up to this long (e.g. 2m)")
	verbose := flag.Bool("v", false, "log progress to stderr")
	flag.Parse()

	if *showVersion {
		fmt.Println("tracklens", version)
		return
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: unknown -tz %q\n", tz)
		os.Exit(1)
	}
	c.tz = loc
	if *verbose {
		c.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	switch err := run(ctx, c, os.Stdout); {
	case errors.Is(err, errFindings):
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func (c config) validate() error {
	switch c.by {
	case "project", "day":
	default:
		return fmt.Errorf("unknown -by %q (want project or day)", c.by)
	}
	switch c.format {
	case "table", "json", "csv":
	default:
		return fmt.Errorf("unknown -format %q (want table, json or csv)", c.format)
	}
	if c.days < 1 {
		return errors.New("-days must be at least 1")
	}
	if c.compare && c.by != "project" {
		return errors.New("-compare reports by project; drop -by day")
	}
	if (c.sync || c.history) && c.dsn == "" {
		return errors.New("-sync and -history need -db or $TRACKLENS_DATABASE_URL")
	}
	if c.sync && c.history {
		return errors.New("-sync and -history are mutually exclusive")
	}
	if c.sync && c.file != "" {
		return errors.New("-sync needs the live API, not -file")
	}
	return nil
}

func run(ctx context.Context, c config, out io.Writer) error {
	if err := c.validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	entries, names, now, err := load(ctx, c, now)
	if err != nil {
		return err
	}

	c.logger().Debug("loaded", "entries", len(entries), "projects", len(names))

	switch {
	case c.compare:
		rows := report.Compare(entries, names, now, now.AddDate(0, 0, -c.days))
		return pick(c.format,
			func() error { return report.WriteCompare(out, rows) },
			func() error { return report.WriteCompareJSON(out, rows) },
			func() error { return report.WriteCompareCSV(out, rows) })
	case c.by == "day":
		rows := report.ByDay(entries, now, c.tz)
		return pick(c.format,
			func() error { return report.WriteDays(out, rows) },
			func() error { return report.WriteDaysJSON(out, rows) },
			func() error { return report.WriteDaysCSV(out, rows) })
	}

	s := report.Summarize(entries, names, now, report.Options{OverlapTolerance: c.overlap})
	if err := pick(c.format, func() error { return s.Write(out) }, func() error { return s.WriteJSON(out) }, func() error { return s.WriteCSV(out) }); err != nil {
		return err
	}
	if c.strict && len(s.Findings) > 0 {
		c.logger().Debug("strict mode: findings present", "count", len(s.Findings))
		return errFindings
	}
	return nil
}

// pick runs the writer for the chosen output format.
func pick(format string, table, jsonOut, csvOut func() error) error {
	switch format {
	case "json":
		return jsonOut()
	case "csv":
		return csvOut()
	}
	return table()
}

func (c config) logger() *slog.Logger {
	if c.log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.log
}

// load picks the data source: fixture file, stored history, or the live API.
func load(ctx context.Context, c config, now time.Time) ([]toggl.Entry, map[int64]string, time.Time, error) {
	// -compare needs the previous window as well.
	back := c.days
	if c.compare {
		back *= 2
	}
	from, to := now.AddDate(0, 0, -back), now.AddDate(0, 0, 1)

	log := c.logger()
	log.Debug("window", "from", from, "to", to)

	if c.file != "" {
		log.Debug("source", "kind", "file", "path", c.file)
		b, err := os.ReadFile(c.file)
		if err != nil {
			return nil, nil, now, err
		}
		var f fixture
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, nil, now, fmt.Errorf("%s: %w", c.file, err)
		}
		names := map[int64]string{}
		for _, p := range f.Projects {
			names[p.ID] = p.Name
		}
		// Fixed clock so sample output is stable.
		fixed := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
		from = fixed.AddDate(0, 0, -back)
		var inWindow []toggl.Entry
		for _, e := range f.Entries {
			if !e.Start.Before(from) {
				inWindow = append(inWindow, e)
			}
		}
		return inWindow, names, fixed, nil
	}

	if c.history {
		log.Debug("source", "kind", "history")
		st, err := store.Open(ctx, c.dsn)
		if err != nil {
			return nil, nil, now, err
		}
		defer func() { _ = st.Close() }()
		entries, err := st.Entries(ctx, from, to)
		if err != nil {
			return nil, nil, now, err
		}
		names, err := st.Projects(ctx)
		return entries, names, now, err
	}

	token := os.Getenv("TOGGL_API_TOKEN")
	if token == "" {
		return nil, nil, now, errors.New("set TOGGL_API_TOKEN (Toggl profile page), or use -file or -history")
	}
	log.Debug("source", "kind", "api")
	api := toggl.New(token)
	if u := os.Getenv("TOGGL_API_URL"); u != "" { // for tests and proxies
		api.BaseURL = u
	}
	entries, err := api.Entries(ctx, from, to)
	if err != nil {
		return nil, nil, now, err
	}
	names, err := api.Projects(ctx)
	if err != nil {
		return nil, nil, now, err
	}
	if c.sync {
		log.Info("syncing", "entries", len(entries), "projects", len(names))
		st, err := store.Open(ctx, c.dsn)
		if err != nil {
			return nil, nil, now, err
		}
		defer func() { _ = st.Close() }()
		if err := st.SaveProjects(ctx, names); err != nil {
			return nil, nil, now, err
		}
		if err := st.SaveEntries(ctx, entries); err != nil {
			return nil, nil, now, err
		}
	}
	return entries, names, now, nil
}
