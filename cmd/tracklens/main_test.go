package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func base() config {
	return config{days: 7, format: "table", by: "project", tz: time.UTC, file: "../../testdata/sample.json"}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*config)
		want string
	}{
		{"bad by", func(c *config) { c.by = "week" }, "-by"},
		{"bad format", func(c *config) { c.format = "xml" }, "-format"},
		{"sync without db", func(c *config) { c.sync = true; c.file = "" }, "-db"},
		{"sync and history", func(c *config) { c.sync, c.history, c.dsn = true, true, "x" }, "mutually exclusive"},
		{"sync with file", func(c *config) { c.sync, c.dsn = true, "x" }, "live API"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mod(&c)
			err := c.validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestRunFixture(t *testing.T) {
	var out strings.Builder
	if err := run(context.Background(), base(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Client portal", "TOTAL", "overlap by 15m0s"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestStrictFailsOnFindings(t *testing.T) {
	c := base()
	c.strict = true
	var out strings.Builder
	if err := run(context.Background(), c, &out); !errors.Is(err, errFindings) {
		t.Fatalf("err = %v, want errFindings", err)
	}
}

func TestMissingTokenIsExplained(t *testing.T) {
	t.Setenv("TOGGL_API_TOKEN", "")
	c := base()
	c.file = ""
	err := run(context.Background(), c, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "TOGGL_API_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRejectsBadCombos(t *testing.T) {
	for name, mod := range map[string]func(*config){
		"zero days":        func(c *config) { c.days = 0 },
		"compare with day": func(c *config) { c.compare, c.by = true, "day" },
	} {
		c := base()
		mod(&c)
		if err := c.validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestFixtureHonoursWindow(t *testing.T) {
	var out strings.Builder
	if err := run(context.Background(), base(), &out); err != nil {
		t.Fatal(err)
	}
	// Entries 90/91 are from the previous week and must not be counted.
	if !strings.Contains(out.String(), "5.75") {
		t.Fatalf("window not applied:\n%s", out.String())
	}
}

func TestCompareFixture(t *testing.T) {
	c := base()
	c.compare = true
	var out strings.Builder
	if err := run(context.Background(), c, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"THIS", "PREVIOUS", "Client portal", "Internal tooling"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
}

func TestOverlapToleranceFlag(t *testing.T) {
	c := base()
	c.overlap = 20 * time.Minute
	var out strings.Builder
	if err := run(context.Background(), c, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "overlap") {
		t.Fatalf("15m overlap should be tolerated:\n%s", out.String())
	}
}

func TestAllViewsInAllFormats(t *testing.T) {
	views := map[string]func(*config){
		"project": func(c *config) {},
		"day":     func(c *config) { c.by = "day" },
		"compare": func(c *config) { c.compare = true },
	}
	for view, setView := range views {
		for _, format := range []string{"table", "json", "csv"} {
			t.Run(view+"/"+format, func(t *testing.T) {
				c := base()
				setView(&c)
				c.format = format
				var out strings.Builder
				if err := run(context.Background(), c, &out); err != nil {
					t.Fatal(err)
				}
				got := out.String()
				switch format {
				case "json":
					var v any
					if err := json.Unmarshal([]byte(got), &v); err != nil {
						t.Fatalf("invalid JSON: %v\n%s", err, got)
					}
				case "csv":
					recs, err := csv.NewReader(strings.NewReader(got)).ReadAll()
					if err != nil || len(recs) < 2 {
						t.Fatalf("invalid CSV (%v):\n%s", err, got)
					}
				default:
					if got == "" {
						t.Fatal("empty table")
					}
				}
			})
		}
	}
}

func TestVerboseLogsToLogger(t *testing.T) {
	var logs strings.Builder
	c := base()
	c.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if err := run(context.Background(), c, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "kind=file") || !strings.Contains(logs.String(), "entries=") {
		t.Fatalf("logs:\n%s", logs.String())
	}
}
