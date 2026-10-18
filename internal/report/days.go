package report

import (
	"io"
	"slices"
	"strings"
	"time"

	"github.com/c-dobkins/p1/internal/toggl"
)

// DayRow is total and billable time for one calendar day.
type DayRow struct {
	Day      string // YYYY-MM-DD in the zone passed to ByDay
	Total    time.Duration
	Billable time.Duration
}

// Days is a per-day breakdown, oldest first.
type Days []DayRow

// ByDay groups time by calendar day in loc. An entry that crosses midnight is
// split so each day gets the time actually worked on it.
func ByDay(entries []toggl.Entry, now time.Time, loc *time.Location) Days {
	byDay := map[string]*DayRow{}
	for _, e := range entries {
		for _, seg := range splitDays(e.Start, e.End(now), loc) {
			r, ok := byDay[seg.day]
			if !ok {
				r = &DayRow{Day: seg.day}
				byDay[seg.day] = r
			}
			r.Total += seg.d
			if e.Billable {
				r.Billable += seg.d
			}
		}
	}
	out := make(Days, 0, len(byDay))
	for _, r := range byDay {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b DayRow) int { return strings.Compare(a.Day, b.Day) })
	return out
}

// Fill returns a row for every calendar day in loc from the day containing
// from through the day containing to, with zero rows for days without time,
// so a chart of the period does not skip empty days.
func (ds Days) Fill(from, to time.Time, loc *time.Location) Days {
	const layout = "2006-01-02"
	have := make(map[string]DayRow, len(ds))
	for _, r := range ds {
		have[r.Day] = r
	}
	last := to.In(loc).Format(layout)
	var out Days
	y, m, d := from.In(loc).Date()
	for day := time.Date(y, m, d, 12, 0, 0, 0, loc); ; day = day.AddDate(0, 0, 1) { // noon: clear of DST jumps
		key := day.Format(layout)
		if key > last {
			break
		}
		r, ok := have[key]
		if !ok {
			r = DayRow{Day: key}
		}
		out = append(out, r)
	}
	return out
}

type segment struct {
	day string
	d   time.Duration
}

// splitDays cuts [start, end) at each local midnight in loc. Midnights come
// from the calendar, so 23- and 25-hour DST days are handled correctly.
func splitDays(start, end time.Time, loc *time.Location) []segment {
	const layout = "2006-01-02"
	if !end.After(start) {
		return []segment{{start.In(loc).Format(layout), 0}}
	}
	var out []segment
	for cur := start.In(loc); cur.Before(end); {
		y, m, d := cur.Date()
		next := time.Date(y, m, d+1, 0, 0, 0, 0, loc)
		if next.After(end) {
			next = end
		}
		out = append(out, segment{cur.Format(layout), next.Sub(cur)})
		cur = next.In(loc)
	}
	return out
}

// WriteTable renders one line per day.
func (ds Days) WriteTable(w io.Writer) error {
	t := newTable("DAY", "HOURS", "BILLABLE")
	for _, r := range ds {
		t.row(r.Day, fmtHours(r.Total), fmtHours(r.Billable))
	}
	return t.flush(w, "")
}

// WriteJSON renders the days as {"days": [...]}.
func (ds Days) WriteJSON(w io.Writer) error {
	type day struct {
		Day      string  `json:"day"`
		Hours    float64 `json:"hours"`
		Billable float64 `json:"billable_hours"`
	}
	out := make([]day, 0, len(ds))
	for _, r := range ds {
		out = append(out, day{r.Day, roundHours(r.Total), roundHours(r.Billable)})
	}
	return writeJSON(w, struct {
		Days []day `json:"days"`
	}{out})
}

// WriteCSV renders one row per day.
func (ds Days) WriteCSV(w io.Writer) error {
	rows := make([][]string, 0, len(ds))
	for _, r := range ds {
		rows = append(rows, []string{r.Day, fmtHours(r.Total), fmtHours(r.Billable)})
	}
	return writeCSV(w, []string{"day", "hours", "billable_hours"}, rows)
}
