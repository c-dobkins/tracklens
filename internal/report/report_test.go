package report

import (
	"io"
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
	s := Summarize(entries, map[int64]string{1: "Portal"}, ts(11, 30), Options{})

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

func TestByDay(t *testing.T) {
	d1 := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(0, 0, 1)
	rows := ByDay([]toggl.Entry{
		{Start: d2, Duration: 1800, Billable: true},
		{Start: d1, Duration: 3600},
		{Start: d1.Add(time.Hour), Duration: 600},
	}, d2, time.UTC)
	if len(rows) != 2 || rows[0].Day != "2026-10-14" || rows[0].Total != 70*time.Minute || rows[1].Billable != 30*time.Minute {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestWriteJSONAndCSV(t *testing.T) {
	s := Summarize([]toggl.Entry{{ID: 1, ProjectID: ptr(int64(1)), Billable: true, Start: ts(9, 0), Duration: 3600}}, map[int64]string{1: "Portal"}, ts(12, 0), Options{})
	var j, c strings.Builder
	if err := s.WriteJSON(&j); err != nil || !strings.Contains(j.String(), `"project": "Portal"`) || !strings.Contains(j.String(), `"findings": []`) {
		t.Fatalf("json: %v\n%s", err, j.String())
	}
	if err := s.WriteCSV(&c); err != nil || c.String() != "project,hours,billable_hours\nPortal,1.00,1.00\n" {
		t.Fatalf("csv: %v\n%q", err, c.String())
	}
}

func TestByDayRespectsTimezone(t *testing.T) {
	// 23:30 UTC on the 14th is already the 15th in Tokyo.
	late := time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	if got := ByDay([]toggl.Entry{{Start: late, Duration: 60}}, late, tokyo); got[0].Day != "2026-10-15" {
		t.Fatalf("day = %s", got[0].Day)
	}
	if got := ByDay([]toggl.Entry{{Start: late, Duration: 60}}, late, time.UTC); got[0].Day != "2026-10-14" {
		t.Fatalf("day = %s", got[0].Day)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWritersReportErrors(t *testing.T) {
	s := Summarize([]toggl.Entry{{ID: 1, Start: ts(9, 0), Duration: 60}}, nil, ts(10, 0), Options{})
	if err := s.Write(failWriter{}); err == nil {
		t.Error("Write: want error")
	}
	if err := s.WriteJSON(failWriter{}); err == nil {
		t.Error("WriteJSON: want error")
	}
	if err := WriteDays(failWriter{}, ByDay(nil, ts(9, 0), time.UTC)); err == nil {
		t.Error("WriteDays: want error")
	}
}

func TestByDaySplitsAtMidnight(t *testing.T) {
	start := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	rows := ByDay([]toggl.Entry{{Start: start, Duration: 4 * 3600, Billable: true}}, start, time.UTC)
	if len(rows) != 2 || rows[0].Total != 2*time.Hour || rows[1].Total != 2*time.Hour || rows[1].Billable != 2*time.Hour {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestByDayRunningTimerSplitsToo(t *testing.T) {
	start := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	now := start.Add(26 * time.Hour) // spans three days
	rows := ByDay([]toggl.Entry{{Start: start, Duration: -1}}, now, time.UTC)
	if len(rows) != 3 || rows[0].Total != time.Hour || rows[1].Total != 24*time.Hour || rows[2].Total != time.Hour {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestByDayDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// US clocks went back on 2026-11-01, so that local day has 25 hours.
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, ny)
	rows := ByDay([]toggl.Entry{{Start: start, Duration: 30 * 3600}}, start, ny)
	if len(rows) != 2 || rows[0].Day != "2026-11-01" || rows[0].Total != 25*time.Hour || rows[1].Total != 5*time.Hour {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestOverlapTolerance(t *testing.T) {
	a := toggl.Entry{ID: 1, Start: ts(9, 0), Stop: ptr(ts(10, 0)), Duration: 3600}
	b := toggl.Entry{ID: 2, Start: ts(9, 59), Stop: ptr(ts(11, 0)), Duration: 3660} // 1 minute early
	if got := overlaps([]toggl.Entry{a, b}, 0); len(got) != 1 {
		t.Fatalf("no tolerance: %v", got)
	}
	if got := overlaps([]toggl.Entry{a, b}, 2*time.Minute); len(got) != 0 {
		t.Fatalf("within tolerance: %v", got)
	}
}

func TestOverlapLongEntrySwallowsShortOnes(t *testing.T) {
	long := toggl.Entry{ID: 1, Start: ts(9, 0), Stop: ptr(ts(17, 0)), Duration: 8 * 3600}
	s1 := toggl.Entry{ID: 2, Start: ts(10, 0), Stop: ptr(ts(10, 30)), Duration: 1800}
	s2 := toggl.Entry{ID: 3, Start: ts(12, 0), Stop: ptr(ts(12, 30)), Duration: 1800}
	got := overlaps([]toggl.Entry{s2, long, s1}, 0)
	// Comparing only neighbours would miss the second one (entry 3 vs entry 2).
	if len(got) != 2 || !strings.Contains(got[1], "entries 1 and 3") {
		t.Fatalf("got %v", got)
	}
}

func TestCompare(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -7)
	one, two := int64(1), int64(2)
	es := []toggl.Entry{
		{Start: now.AddDate(0, 0, -1), Duration: 4 * 3600, ProjectID: &one},
		{Start: now.AddDate(0, 0, -9), Duration: 2 * 3600, ProjectID: &one},
		{Start: now.AddDate(0, 0, -2), Duration: 3600, ProjectID: &two}, // new this week
		{Start: now.AddDate(0, 0, -10), Duration: 5 * 3600},             // only last week
	}
	rows := Compare(es, map[int64]string{1: "A", 2: "B"}, now, cutoff)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Project != noProject || rows[0].Delta() != -5*time.Hour {
		t.Fatalf("biggest mover first: %+v", rows[0])
	}
	var out strings.Builder
	if err := WriteCompare(&out, rows); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+100%", "new", "-100%", "TOTAL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
}

func TestCompareJSONAndCSVNewProject(t *testing.T) {
	rows := []CompareRow{{Project: "Old", Current: time.Hour, Previous: 2 * time.Hour}, {Project: "Fresh", Current: time.Hour}}
	var j, c strings.Builder
	if err := WriteCompareJSON(&j, rows); err != nil {
		t.Fatal(err)
	}
	// A project with no previous time has no meaningful percentage.
	if !strings.Contains(j.String(), `"change_percent": -50`) || !strings.Contains(j.String(), `"change_percent": null`) {
		t.Fatalf("json:\n%s", j.String())
	}
	if err := WriteCompareCSV(&c, rows); err != nil {
		t.Fatal(err)
	}
	if want := "project,current_hours,previous_hours,change_hours,change_percent\nOld,1.00,2.00,-1.00,-50.0\nFresh,1.00,0.00,1.00,\n"; c.String() != want {
		t.Fatalf("csv = %q", c.String())
	}
}

func TestDaysJSONAndCSV(t *testing.T) {
	rows := []DayRow{{Day: "2026-10-15", Total: 90 * time.Minute, Billable: time.Hour}}
	var j, c strings.Builder
	if err := WriteDaysJSON(&j, rows); err != nil || !strings.Contains(j.String(), `"hours": 1.5`) {
		t.Fatalf("json: %v\n%s", err, j.String())
	}
	if err := WriteDaysCSV(&c, rows); err != nil || c.String() != "day,hours,billable_hours\n2026-10-15,1.50,1.00\n" {
		t.Fatalf("csv: %v %q", err, c.String())
	}
}

func TestOutputRoundsToTwoDecimals(t *testing.T) {
	var j strings.Builder
	if err := WriteDaysJSON(&j, []DayRow{{Day: "d", Total: 20 * time.Minute}}); err != nil || !strings.Contains(j.String(), `"hours": 0.33`) {
		t.Fatalf("%v\n%s", err, j.String())
	}
}
