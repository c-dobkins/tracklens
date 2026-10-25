package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/c-dobkins/tracklens/internal/store"
	"github.com/c-dobkins/tracklens/internal/testdb"
	"github.com/c-dobkins/tracklens/internal/toggl"
)

var listening = regexp.MustCompile(`msg=listening addr=(\S+)`)

// startServe runs the real -serve command on a random port and returns its
// address. Cleanup cancels it and checks for a graceful, successful exit.
func startServe(t *testing.T, vars map[string]string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	logR, logW := io.Pipe()
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, append([]string{"-serve", "127.0.0.1:0"}, args...), mapEnv(vars), io.Discard, logW)
		_ = logW.Close()
	}()

	addr := ""
	scanner := bufio.NewScanner(logR)
	for addr == "" && scanner.Scan() {
		if m := listening.FindStringSubmatch(scanner.Text()); m != nil {
			addr = m[1]
		}
	}
	go func() { _, _ = io.Copy(io.Discard, logR) }() // keep request logs flowing
	t.Cleanup(func() {
		cancel()
		select {
		case code := <-done:
			if code != exitOK {
				t.Errorf("serve exited %d", code)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down")
		}
	})
	if addr == "" {
		t.Fatal("server never reported its address")
	}
	return addr
}

// totalHours fetches /v1/summary and returns total_hours.
func totalHours(t *testing.T, addr string) float64 {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/v1/summary", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Hours float64 `json:"total_hours"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Hours
}

func TestServeFixture(t *testing.T) {
	addr := startServe(t, nil, "-file", sample)
	// Same total as the CLI: the fixture's clock is used as-is, so the running
	// timer counts exactly one hour.
	if got := totalHours(t, addr); got != 5.75 {
		t.Fatalf("total %v", got)
	}
}

func TestServeHistory(t *testing.T) {
	dsn := testdb.DSN(t)
	st, err := store.Open(t.Context(), dsn) // migrate and seed as a writer would
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-2 * time.Hour)
	seed := []toggl.Entry{{ID: 1, Start: start, Stop: new(start.Add(90 * time.Minute)), Duration: 5400}}
	if _, err := st.Sync(t.Context(), start.Add(-time.Hour), start.Add(time.Hour), seed, store.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	addr := startServe(t, map[string]string{"TRACKLENS_DATABASE_URL": dsn}, "-history")
	if got := totalHours(t, addr); got != 1.5 {
		t.Fatalf("total %v", got)
	}
}

func TestServeRefusesUnmigratedDatabase(t *testing.T) {
	dsn := testdb.DSN(t)
	code, _, errOut := runCLI(t, map[string]string{"TRACKLENS_DATABASE_URL": dsn}, "-serve", "127.0.0.1:0", "-history")
	if code != exitError || !regexp.MustCompile(`schema is not up to date`).MatchString(errOut) {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}
