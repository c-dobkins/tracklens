package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/toggl"
)

func at(h, m int) time.Time { return time.Date(2026, 10, 15, h, m, 0, 0, time.UTC) }

// entry builds a stopped entry from a start time and length.
func entry(id int64, start time.Time, d time.Duration) toggl.Entry {
	return toggl.Entry{ID: id, Start: start, Stop: new(start.Add(d)), Duration: int64(d / time.Second)}
}

func kinds(fs []Finding) []Kind {
	out := make([]Kind, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Kind)
	}
	return out
}

func TestSummarize(t *testing.T) {
	billable := entry(1, at(9, 0), time.Hour)
	billable.ProjectID, billable.Billable = new(int64(1)), true
	overlapping := entry(2, at(9, 45), 45*time.Minute)
	overlapping.ProjectID = new(int64(1))
	running := toggl.Entry{ID: 3, Start: at(11, 0), Duration: -1}

	s := Summarize([]toggl.Entry{billable, overlapping, running}, map[int64]string{1: "Portal"}, at(11, 30), Options{})

	if want := time.Hour + 45*time.Minute + 30*time.Minute; s.Total != want || s.Billable != time.Hour {
		t.Fatalf("total %v billable %v", s.Total, s.Billable)
	}
	if len(s.Rows) != 2 || s.Rows[0].Project != "Portal" || s.Rows[1].Project != noProject {
		t.Fatalf("rows = %+v", s.Rows)
	}
	if got := kinds(s.Findings); !slices.Equal(got, []Kind{KindRunning, KindNoProject, KindOverlap}) {
		t.Fatalf("finding kinds = %v", got)
	}
	if ids := s.Findings[2].EntryIDs; !slices.Equal(ids, []int64{1, 2}) {
		t.Fatalf("overlap ids = %v", ids)
	}
}

func TestSummarizeOrderIsDeterministic(t *testing.T) {
	var es []toggl.Entry
	for i, name := range []string{"C", "A", "B"} {
		e := entry(int64(i), at(9+i, 0), time.Hour) // equal totals
		e.ProjectID, e.ProjectName = new(int64(i)), name
		es = append(es, e)
	}
	for range 20 { // map iteration order would show up within a few runs
		s := Summarize(es, nil, at(18, 0), Options{})
		if got := []string{s.Rows[0].Project, s.Rows[1].Project, s.Rows[2].Project}; !slices.Equal(got, []string{"A", "B", "C"}) {
			t.Fatalf("order = %v", got)
		}
	}
}

func TestProjectNameFallbacks(t *testing.T) {
	withMeta := toggl.Entry{ProjectID: new(int64(7)), ProjectName: "From API"}
	if got := projectName(withMeta, nil); got != "From API" {
		t.Errorf("meta name: %q", got)
	}
	if got := projectName(withMeta, map[int64]string{7: "From store"}); got != "From store" {
		t.Errorf("names map wins: %q", got)
	}
	if got := projectName(toggl.Entry{ProjectID: new(int64(8))}, nil); got != "project 8" {
		t.Errorf("unknown: %q", got)
	}
}

func TestOverlaps(t *testing.T) {
	cases := []struct {
		name      string
		entries   []toggl.Entry
		tolerance time.Duration
		want      []string
	}{
		{
			name:    "adjacent entries do not overlap",
			entries: []toggl.Entry{entry(1, at(9, 0), time.Hour), entry(2, at(10, 0), time.Hour)},
		},
		{
			name:    "one minute early",
			entries: []toggl.Entry{entry(1, at(9, 0), time.Hour), entry(2, at(9, 59), time.Hour)},
			want:    []string{"entries 1 and 2 overlap by 1m0s"},
		},
		{
			name:      "within tolerance",
			entries:   []toggl.Entry{entry(1, at(9, 0), time.Hour), entry(2, at(9, 59), time.Hour)},
			tolerance: 2 * time.Minute,
		},
		{
			name: "long entry covers several short ones, input unsorted",
			entries: []toggl.Entry{
				entry(3, at(12, 0), 30*time.Minute), entry(1, at(9, 0), 8*time.Hour), entry(2, at(10, 0), 30*time.Minute),
			},
			// Comparing only neighbours would miss entry 3; and a contained
			// entry overlaps by its own length, not to the end of the long one.
			want: []string{"entries 1 and 2 overlap by 30m0s", "entries 1 and 3 overlap by 30m0s"},
		},
		{
			name:    "running timers are ignored",
			entries: []toggl.Entry{entry(1, at(9, 0), time.Hour), {ID: 2, Start: at(9, 30), Duration: -1}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, f := range overlaps(tc.entries, tc.tolerance) {
				got = append(got, f.Message)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestByDay(t *testing.T) {
	late := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	billable := entry(1, late, 4*time.Hour) // 22:00-02:00
	billable.Billable = true

	t.Run("splits at midnight", func(t *testing.T) {
		got := ByDay([]toggl.Entry{billable}, late, time.UTC)
		want := Days{{"2026-10-14", 2 * time.Hour, 2 * time.Hour}, {"2026-10-15", 2 * time.Hour, 2 * time.Hour}}
		if !slices.Equal(got, want) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("running timer split up to now", func(t *testing.T) {
		start := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
		got := ByDay([]toggl.Entry{{Start: start, Duration: -1}}, start.Add(26*time.Hour), time.UTC)
		if len(got) != 3 || got[0].Total != time.Hour || got[1].Total != 24*time.Hour || got[2].Total != time.Hour {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("time zone", func(t *testing.T) {
		tokyo, err := time.LoadLocation("Asia/Tokyo")
		if err != nil {
			t.Skip("tzdata unavailable")
		}
		got := ByDay([]toggl.Entry{entry(1, time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC), time.Minute)}, late, tokyo)
		if got[0].Day != "2026-10-15" {
			t.Fatalf("day = %s", got[0].Day)
		}
	})
	t.Run("25-hour DST day", func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("tzdata unavailable")
		}
		start := time.Date(2026, 11, 1, 0, 0, 0, 0, ny) // US clocks go back this day
		got := ByDay([]toggl.Entry{entry(1, start, 30*time.Hour)}, start, ny)
		if len(got) != 2 || got[0].Day != "2026-11-01" || got[0].Total != 25*time.Hour || got[1].Total != 5*time.Hour {
			t.Fatalf("got %+v", got)
		}
	})
}

// FuzzSplitDays checks that splitting never loses or invents time, and that
// each piece is one calendar day or less, in order.
func FuzzSplitDays(f *testing.F) {
	f.Add(int64(1792018800), int64(3600), "UTC")
	f.Add(int64(1793512800), int64(108000), "America/New_York")
	f.Add(int64(1774738800), int64(90000), "Europe/Tallinn")
	f.Fuzz(func(t *testing.T, startUnix, seconds int64, zone string) {
		loc, err := time.LoadLocation(zone)
		if err != nil || seconds < 0 || seconds > 60*24*3600 || startUnix < 0 || startUnix > 4e9 {
			t.Skip()
		}
		start := time.Unix(startUnix, 0)
		end := start.Add(time.Duration(seconds) * time.Second)
		segs := splitDays(start, end, loc)

		var sum time.Duration
		for i, s := range segs {
			sum += s.d
			if s.d < 0 || s.d > 25*time.Hour {
				t.Fatalf("segment %d has length %v", i, s.d)
			}
			if i > 0 && s.day <= segs[i-1].day {
				t.Fatalf("days not increasing: %s then %s", segs[i-1].day, s.day)
			}
		}
		if sum != end.Sub(start) {
			t.Fatalf("segments sum to %v, want %v", sum, end.Sub(start))
		}
	})
}

func TestCompare(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	project := func(e toggl.Entry, id int64) toggl.Entry { e.ProjectID = new(id); return e }
	es := []toggl.Entry{
		project(entry(1, now.AddDate(0, 0, -1), 4*time.Hour), 1),
		project(entry(2, now.AddDate(0, 0, -9), 2*time.Hour), 1),
		project(entry(3, now.AddDate(0, 0, -2), time.Hour), 2), // new this week
		entry(4, now.AddDate(0, 0, -10), 5*time.Hour),          // only last week
	}
	from, cutoff := Periods(now, 7)
	rows := Compare(es, map[int64]string{1: "A", 2: "B"}, now, from, cutoff)
	want := Comparison{{noProject, 0, 5 * time.Hour}, {"A", 4 * time.Hour, 2 * time.Hour}, {"B", time.Hour, 0}}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %+v", rows)
	}

	var table strings.Builder
	if err := rows.WriteTable(&table); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(table.String()), "\n")
	wantLines := [][]string{
		{"PROJECT", "THIS", "PREVIOUS", "CHANGE", "CHANGE", "%"},
		{"(no", "project)", "0.00", "5.00", "-5.00", "-100%"},
		{"A", "4.00", "2.00", "+2.00", "+100%"},
		{"B", "1.00", "0.00", "+1.00", "new"},
		{"TOTAL", "5.00", "7.00", "-2.00", "-29%"},
	}
	if len(lines) != len(wantLines) {
		t.Fatalf("table:\n%s", table.String())
	}
	for i, w := range wantLines {
		if got := strings.Fields(lines[i]); !slices.Equal(got, w) {
			t.Errorf("line %d = %q, want %q", i, got, w)
		}
	}
}

func TestComparisonJSONAndCSV(t *testing.T) {
	rows := Comparison{{"Old", time.Hour, 2 * time.Hour}, {"Fresh", time.Hour, 0}}
	var j, c strings.Builder
	if err := rows.WriteJSON(&j); err != nil {
		t.Fatal(err)
	}
	// A project with no previous time has no meaningful percentage.
	if !strings.Contains(j.String(), `"change_percent": -50`) || !strings.Contains(j.String(), `"change_percent": null`) {
		t.Fatalf("json:\n%s", j.String())
	}
	if err := rows.WriteCSV(&c); err != nil {
		t.Fatal(err)
	}
	want := "project,current_hours,previous_hours,change_hours,change_percent\nOld,1.00,2.00,-1.00,-50.0\nFresh,1.00,0.00,1.00,\n"
	if c.String() != want {
		t.Fatalf("csv = %q", c.String())
	}
}

func TestSummaryJSONHasStructuredFindings(t *testing.T) {
	s := Summarize([]toggl.Entry{{ID: 9, Start: at(9, 0), Duration: -1}}, nil, at(9, 20), Options{})
	var out struct {
		Hours    float64   `json:"total_hours"`
		Findings []Finding `json:"findings"`
	}
	var b bytes.Buffer
	if err := s.WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// 20 minutes rounds to 0.33 hours in every output.
	if out.Hours != 0.33 || len(out.Findings) != 2 || out.Findings[0].Kind != KindRunning || out.Findings[0].EntryIDs[0] != 9 {
		t.Fatalf("decoded %+v from\n%s", out, b.String())
	}
}

func TestEmptyReportsAreValid(t *testing.T) {
	for name, r := range map[string]Renderer{
		"summary": Summarize(nil, nil, at(9, 0), Options{}),
		"days":    ByDay(nil, at(9, 0), time.UTC),
		"compare": Compare(nil, nil, at(9, 0), at(7, 0), at(8, 0)),
	} {
		var j, c bytes.Buffer
		if err := r.WriteJSON(&j); err != nil || !json.Valid(j.Bytes()) || strings.Contains(j.String(), "null") {
			t.Errorf("%s: JSON %v\n%s", name, err, j.String())
		}
		if err := r.WriteCSV(&c); err != nil {
			t.Errorf("%s: CSV %v", name, err)
		}
		if recs, err := csv.NewReader(&c).ReadAll(); err != nil || len(recs) != 1 {
			t.Errorf("%s: want header only, got %v %v", name, recs, err)
		}
	}
}

func TestParseFormatAndRender(t *testing.T) {
	for _, s := range []string{"table", "json", "csv"} {
		if f, err := ParseFormat(s); err != nil || string(f) != s {
			t.Errorf("ParseFormat(%q) = %q, %v", s, f, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("ParseFormat(xml): want error")
	}
	if err := Render(io.Discard, Format("xml"), Days{}); err == nil {
		t.Error("Render(xml): want error")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWritersReportErrors(t *testing.T) {
	views := map[string]Renderer{
		"summary": Summarize([]toggl.Entry{entry(1, at(9, 0), time.Minute)}, nil, at(10, 0), Options{}),
		"days":    Days{{Day: "d"}},
		"compare": Comparison{{Project: "p"}},
	}
	for name, r := range views {
		for _, f := range []Format{FormatTable, FormatJSON, FormatCSV} {
			if err := Render(failWriter{}, f, r); err == nil {
				t.Errorf("%s/%s: want error", name, f)
			}
		}
	}
}

// The table rounds the raw percentage once; rounding the one-decimal JSON
// value again would turn 22.46% into +23%.
func TestPercentRoundedOnce(t *testing.T) {
	prev := 10000 * time.Second
	rows := Comparison{{"P", prev + 2246*time.Second, prev}} // +22.46%
	var table, csvOut strings.Builder
	if err := rows.WriteTable(&table); err != nil {
		t.Fatal(err)
	}
	if err := rows.WriteCSV(&csvOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), "+22%") || !strings.HasSuffix(csvOut.String(), ",22.5\n") {
		t.Fatalf("table:\n%s\ncsv:\n%s", table.String(), csvOut.String())
	}
}

func TestCompareSplitsEntriesAcrossTheCutoff(t *testing.T) {
	cutoff := at(12, 0)
	es := []toggl.Entry{
		entry(1, at(11, 0), 3*time.Hour),         // 1h before, 2h after
		entry(2, at(9, 0), 3*time.Hour),          // ends exactly at the cutoff
		{ID: 3, Start: at(11, 30), Duration: -1}, // running across the cutoff
	}
	got := Compare(es, nil, at(13, 0), at(8, 0), cutoff)
	want := Comparison{{noProject, 2*time.Hour + time.Hour, time.Hour + 3*time.Hour + 30*time.Minute}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// Time before the previous period is left out at that edge too.
	early := entry(4, at(7, 0), 2*time.Hour) // 07:00-09:00, previous period starts 08:00
	got = Compare([]toggl.Entry{early}, nil, at(13, 0), at(8, 0), cutoff)
	want = Comparison{{noProject, 0, time.Hour}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFilterProject(t *testing.T) {
	es := []toggl.Entry{
		{ID: 1, ProjectID: new(int64(1))},
		{ID: 2, ProjectID: new(int64(2)), ProjectName: "Client Portal v2"}, // name only on the entry
		{ID: 3, ProjectID: new(int64(3))},
		{ID: 4}, // no project never matches
	}
	got := FilterProject(es, map[int64]string{1: "Client portal", 3: "Internal"}, "PORTAL")
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
		t.Fatalf("got %+v", got)
	}
	// An unnamed project must not match its placeholder label "project 9".
	if got := FilterProject([]toggl.Entry{{ID: 5, ProjectID: new(int64(9))}}, nil, "project"); len(got) != 0 {
		t.Fatalf("placeholder matched: %+v", got)
	}
}

func TestWindow(t *testing.T) {
	now := at(12, 0)
	from, to := Window(now, 7, false)
	if !from.Equal(now.AddDate(0, 0, -7)) || !to.Equal(now.Add(toggl.ClockSkew)) {
		t.Fatalf("report window %v..%v", from, to)
	}
	from, _ = Window(now, 7, true)
	if !from.Equal(now.AddDate(0, 0, -14).Add(-compareMargin)) {
		t.Fatalf("compare window starts %v", from)
	}
	if pFrom, cutoff := Periods(now, 7); !pFrom.Equal(now.AddDate(0, 0, -14)) || !cutoff.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("periods %v %v", pFrom, cutoff)
	}
}

func TestDaysFill(t *testing.T) {
	have := Days{{Day: "2026-10-14", Total: time.Hour}}
	got := have.Fill(time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), time.UTC)
	want := Days{{Day: "2026-10-12"}, {Day: "2026-10-13"}, {Day: "2026-10-14", Total: time.Hour}, {Day: "2026-10-15"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}

	// Across a DST change the calendar still advances one day at a time.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	got = Days{}.Fill(time.Date(2026, 10, 31, 23, 0, 0, 0, ny), time.Date(2026, 11, 2, 1, 0, 0, 0, ny), ny)
	if len(got) != 3 || got[1].Day != "2026-11-01" {
		t.Fatalf("DST: %+v", got)
	}
}
