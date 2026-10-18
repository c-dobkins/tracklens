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
	flag.Parse()

	if err := run(*days, *file); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(days int, file string) error {
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

	report.Summarize(entries, names, now).Write(os.Stdout)
	return nil
}
