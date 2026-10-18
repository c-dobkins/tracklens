package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

const sample = "../../testdata/sample.json"

// mapEnv is an env backed by a map, so tests never see the real environment.
func mapEnv(m map[string]string) env { return func(k string) string { return m[k] } }

// runCLI runs the command and returns exit code, stdout and stderr.
func runCLI(t *testing.T, vars map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, mapEnv(vars), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestGolden(t *testing.T) {
	views := map[string][]string{
		"project": nil,
		"day":     {"-by", "day"},
		"compare": {"-compare"},
	}
	for view, extra := range views {
		for _, format := range []string{"table", "json", "csv"} {
			name := view + "." + format
			t.Run(name, func(t *testing.T) {
				args := append([]string{"-file", sample, "-format", format}, extra...)
				code, out, errOut := runCLI(t, nil, args...)
				if code != exitOK {
					t.Fatalf("exit %d: %s", code, errOut)
				}
				path := filepath.Join("testdata", name+".golden")
				if *update {
					if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run go test -update to create it)", err)
				}
				if out != string(want) {
					t.Errorf("output differs from %s:\n--- got\n%s--- want\n%s", path, out, want)
				}
			})
		}
	}
}

func TestUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"unknown flag":       {"-nope"},
		"stray argument":     {"extra"},
		"bad format":         {"-format", "xml"},
		"bad by":             {"-by", "week"},
		"bad tz":             {"-tz", "Mars/Olympus"},
		"zero days":          {"-days", "0"},
		"negative tolerance": {"-overlap-tolerance", "-1m"},
		"zero timeout":       {"-timeout", "0s"},
		"compare by day":     {"-compare", "-by", "day"},
		"sync and history":   {"-sync", "-history", "-db", "x"},
		"sync with file":     {"-sync", "-db", "x", "-file", sample},
		"history no db":      {"-history"},
		"sync with project":  {"-sync", "-db", "x", "-project", "a"},
		"prune without sync": {"-prune", "-db", "x"},
		"serve with sync":    {"-serve", ":0", "-sync", "-db", "x"},
		"serve without data": {"-serve", ":0"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			code, out, errOut := runCLI(t, nil, args...)
			if code != exitUsage || out != "" || errOut == "" {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

func TestHelpAndVersion(t *testing.T) {
	if code, _, errOut := runCLI(t, nil, "-h"); code != exitOK || !strings.Contains(errOut, "Exit status") {
		t.Fatalf("help: exit %d\n%s", code, errOut)
	}
	if code, out, _ := runCLI(t, nil, "-version"); code != exitOK || out != "tracklens dev\n" {
		t.Fatalf("version: exit %d %q", code, out)
	}
}

func TestStrictExitCode(t *testing.T) {
	code, out, _ := runCLI(t, nil, "-file", sample, "-strict")
	if code != exitFindings || !strings.Contains(out, "Checks:") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// Findings only exist in the project view, so -strict is quiet elsewhere.
	if code, _, _ := runCLI(t, nil, "-file", sample, "-strict", "-by", "day"); code != exitOK {
		t.Fatalf("by day: exit %d", code)
	}
}

func TestOverlapTolerance(t *testing.T) {
	_, out, _ := runCLI(t, nil, "-file", sample, "-overlap-tolerance", "20m")
	if strings.Contains(out, "overlap") {
		t.Fatalf("15m overlap should be tolerated:\n%s", out)
	}
}

func TestProjectFilter(t *testing.T) {
	_, out, _ := runCLI(t, nil, "-file", sample, "-project", "PORTAL")
	if !strings.Contains(out, "Client portal") || strings.Contains(out, "Internal tooling") || strings.Contains(out, "(no project)") {
		t.Fatalf("filter not applied:\n%s", out)
	}
}

func TestMissingTokenIsExplained(t *testing.T) {
	code, _, errOut := runCLI(t, nil)
	if code != exitError || !strings.Contains(errOut, "TOGGL_API_TOKEN") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestBadFixtureIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runCLI(t, nil, "-file", path); code != exitError || !strings.Contains(errOut, "bad.json") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if code, _, _ := runCLI(t, nil, "-file", filepath.Join(t.TempDir(), "missing.json")); code != exitError {
		t.Fatalf("missing file: exit %d", code)
	}
}

func TestVerboseLogging(t *testing.T) {
	_, _, quiet := runCLI(t, nil, "-file", sample)
	_, _, loud := runCLI(t, nil, "-file", sample, "-v")
	if quiet != "" || !strings.Contains(loud, "kind=file") {
		t.Fatalf("quiet %q, verbose %q", quiet, loud)
	}
}

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFixtureRejectsUnknownFields(t *testing.T) {
	old := writeFixture(t, `{"projects": [], "entries": []}`) // the pre-meta format
	if code, _, errOut := runCLI(t, nil, "-file", old); code != exitError || !strings.Contains(errOut, "projects") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestEmptyWindowWarns(t *testing.T) {
	stale := writeFixture(t, `{"entries": [{"id": 1, "start": "2020-01-01T09:00:00Z", "duration": 60}]}`)
	code, _, errOut := runCLI(t, nil, "-file", stale)
	if code != exitOK || !strings.Contains(errOut, "no fixture entries fall in the window") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestClockSkewSlack(t *testing.T) {
	// The entry starts two minutes after "now", as if its device clock ran fast.
	ahead := writeFixture(t, `{"now": "2026-10-16T18:00:00Z",
		"entries": [{"id": 1, "start": "2026-10-16T18:02:00Z", "duration": -1}]}`)
	_, out, _ := runCLI(t, nil, "-file", ahead, "-format", "json")
	if !strings.Contains(out, `"kind": "running"`) {
		t.Fatalf("entry ahead of the clock was dropped:\n%s", out)
	}
}
