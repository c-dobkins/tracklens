package report

import (
	"strings"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/toggl"
)

func ts(h, m int) time.Time { return time.Date(2026, 10, 15, h, m, 0, 0, time.UTC) }
func ptr[T any](v T) *T     { return &v }

func TestSummarize(t *testing.T) {
	a, b := ts(9, 0), ts(10, 0)
	c, d := ts(9, 45), ts(10, 30)
	entries := []toggl.Entry{
		{ID: 1, ProjectID: ptr(int64(1)), Billable: true, Start: a, Stop: &b, Duration: 3600},
		{ID: 2, ProjectID: ptr(int64(1)), Billable: false, Start: c, Stop: &d, Duration: 2700},
		{ID: 3, Start: ts(11, 0), Duration: -1},
	}
	s := Summarize(entries, map[int64]string{1: "Portal"}, ts(11, 30))

	// 1h + 45m + 30m running
	if s.Total != 2*time.Hour+15*time.Minute {
		t.Fatalf("total = %v", s.Total)
	}
	if s.Billable != time.Hour {
		t.Fatalf("billable = %v", s.Billable)
	}
	var out strings.Builder
	s.Write(&out)
	for _, want := range []string{"Portal", "still running", "has no project", "overlap by 15m0s"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}
