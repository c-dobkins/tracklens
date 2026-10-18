package main

import (
	"context"
	"errors"
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
