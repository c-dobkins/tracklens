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
)

// Starts the real -serve command on a random port, queries it, then cancels
// the context and expects a graceful exit.
func TestServeFixture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logR, logW := io.Pipe()
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-serve", "127.0.0.1:0", "-file", sample}, mapEnv(nil), io.Discard, logW)
		_ = logW.Close()
	}()

	addr := ""
	scanner := bufio.NewScanner(logR)
	for addr == "" && scanner.Scan() {
		if m := regexp.MustCompile(`msg=listening addr=(\S+)`).FindStringSubmatch(scanner.Text()); m != nil {
			addr = m[1]
		}
	}
	if addr == "" {
		t.Fatal("server never reported its address")
	}
	go func() { _, _ = io.Copy(io.Discard, logR) }() // keep request logs flowing

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/v1/summary", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Hours float64 `json:"total_hours"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// Same totals as the CLI: the fixture's clock is used as-is, so the running
	// timer counts exactly one hour.
	if resp.StatusCode != http.StatusOK || body.Hours != 5.75 {
		t.Fatalf("status %d, total %v", resp.StatusCode, body.Hours)
	}

	cancel()
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}
