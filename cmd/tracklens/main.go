// tracklens summarises Toggl Track time entries and flags data-quality issues.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/c-dobkins/p1/internal/report"
	"github.com/c-dobkins/p1/internal/toggl"
)

type fixture struct {
	Projects []toggl.Project `json:"projects"`
	Entries  []toggl.Entry   `json:"entries"`
}

func main() {
	days := flag.Int("days", 7, "number of days to look back")
	file := flag.String("file", "", "read entries from a JSON fixture instead of the API")
	format := flag.String("format", "table", "output format: table, json or csv")
	by := flag.String("by", "project", "grouping: project or day")
	flag.Parse()

	if err := run(*days, *file, *format, *by); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(days int, file, format, by string) error {
	if by != "project" && by != "day" {
		return fmt.Errorf("unknown -by %q (want project or day)", by)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	now := time.Now().UTC()

	var entries []toggl.Entry
	names := map[int64]string{}

	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var f fixture
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		entries = f.Entries
		for _, p := range f.Projects {
			names[p.ID] = p.Name
		}
		now = time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC) // fixed so the sample output is stable
	} else {
		token := os.Getenv("TOGGL_API_TOKEN")
		if token == "" {
			return fmt.Errorf("set TOGGL_API_TOKEN (Toggl profile page) or pass -file")
		}
		c := toggl.New(token)
		var err error
		if entries, err = c.Entries(ctx, now.AddDate(0, 0, -days), now.AddDate(0, 0, 1)); err != nil {
			return err
		}
		if names, err = c.Projects(ctx); err != nil {
			return err
		}
	}

	if by == "day" {
		report.WriteDays(os.Stdout, report.ByDay(entries, now))
		return nil
	}
	s := report.Summarize(entries, names, now)
	switch format {
	case "table":
		s.Write(os.Stdout)
	case "json":
		return s.WriteJSON(os.Stdout)
	case "csv":
		return s.WriteCSV(os.Stdout)
	default:
		return fmt.Errorf("unknown -format %q (want table, json or csv)", format)
	}
	return nil
}
