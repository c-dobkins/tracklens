// Package report turns raw time entries into a summary plus data-quality findings.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/c-dobkins/p1/internal/toggl"
)

type Row struct {
	Project  string
	Total    time.Duration
	Billable time.Duration
}

type Summary struct {
	Rows     []Row
	Total    time.Duration
	Billable time.Duration
	Findings []string
}

const noProject = "(no project)"

// Options tunes the data-quality checks.
type Options struct {
	// OverlapTolerance ignores overlaps up to this long, which absorbs the
	// small clock slop between a timer stopping and the next one starting.
	OverlapTolerance time.Duration
}

// Summarize groups entries by project. now is used to size running timers.
func Summarize(entries []toggl.Entry, names map[int64]string, now time.Time, opts Options) Summary {
	byProject := map[string]*Row{}
	var s Summary
	var closed []toggl.Entry

	for _, e := range entries {
		d := time.Duration(e.Duration) * time.Second
		if e.Duration < 0 { // running timer
			d = now.Sub(e.Start)
			s.Findings = append(s.Findings, fmt.Sprintf("entry %d is still running (%s so far)", e.ID, d.Round(time.Minute)))
		} else {
			closed = append(closed, e)
		}
		name := projectName(e, names)
		if e.ProjectID == nil {
			s.Findings = append(s.Findings, fmt.Sprintf("entry %d %q has no project", e.ID, e.Description))
		}
		r := byProject[name]
		if r == nil {
			r = &Row{Project: name}
			byProject[name] = r
		}
		r.Total += d
		s.Total += d
		if e.Billable {
			r.Billable += d
			s.Billable += d
		}
	}

	for _, r := range byProject {
		s.Rows = append(s.Rows, *r)
	}
	sort.Slice(s.Rows, func(i, j int) bool { return s.Rows[i].Total > s.Rows[j].Total })
	s.Findings = append(s.Findings, overlaps(closed, opts.OverlapTolerance)...)
	return s
}

// overlaps reports entries that start before the latest-ending earlier entry
// has finished. Tracking the furthest end seen so far (rather than just the
// previous entry) also catches one long entry that swallows several short ones.
func overlaps(es []toggl.Entry, tolerance time.Duration) []string {
	sorted := append([]toggl.Entry(nil), es...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })

	var out []string
	var latest *toggl.Entry // entry with the furthest end so far
	for i := range sorted {
		cur := &sorted[i]
		if cur.Stop == nil {
			continue
		}
		if latest != nil {
			if by := latest.Stop.Sub(cur.Start); by > tolerance {
				out = append(out, fmt.Sprintf("entries %d and %d overlap by %s", latest.ID, cur.ID, by.Round(time.Second)))
			}
		}
		if latest == nil || cur.Stop.After(*latest.Stop) {
			latest = cur
		}
	}
	return out
}

func projectName(e toggl.Entry, names map[int64]string) string {
	if e.ProjectID == nil {
		return noProject
	}
	if n, ok := names[*e.ProjectID]; ok {
		return n
	}
	return fmt.Sprintf("project %d", *e.ProjectID)
}

func hours(d time.Duration) string { return fmt.Sprintf("%.2f", d.Hours()) }

// Write renders the table and any findings. Output is built in memory first
// so a failing writer is reported once, as an error.
func (s Summary) Write(w io.Writer) error {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PROJECT\tHOURS\tBILLABLE\tBILLABLE %")
	for _, r := range s.Rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Project, hours(r.Total), hours(r.Billable), pct(r.Billable, r.Total))
	}
	_, _ = fmt.Fprintf(tw, "TOTAL\t%s\t%s\t%s\n", hours(s.Total), hours(s.Billable), pct(s.Billable, s.Total))
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(s.Findings) > 0 {
		b.WriteString("\nChecks:\n")
		for _, f := range s.Findings {
			b.WriteString(" - " + f + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func pct(a, b time.Duration) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(a)/float64(b))
}

// DayRow is total and billable time for one UTC calendar day.
type DayRow struct {
	Day      string
	Total    time.Duration
	Billable time.Duration
}

// ByDay groups time by calendar day in loc, oldest first. An entry that
// crosses midnight is split so each day gets the time actually worked on it.
func ByDay(entries []toggl.Entry, now time.Time, loc *time.Location) []DayRow {
	days := map[string]*DayRow{}
	for _, e := range entries {
		end := e.Start.Add(time.Duration(e.Duration) * time.Second)
		if e.Duration < 0 {
			end = now
		}
		for _, seg := range splitDays(e.Start, end, loc) {
			r := days[seg.day]
			if r == nil {
				r = &DayRow{Day: seg.day}
				days[seg.day] = r
			}
			r.Total += seg.d
			if e.Billable {
				r.Billable += seg.d
			}
		}
	}
	out := make([]DayRow, 0, len(days))
	for _, r := range days {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out
}

type segment struct {
	day string
	d   time.Duration
}

// splitDays cuts [start, end) at each local midnight in loc. Midnights are
// computed from the calendar, so 23- and 25-hour DST days come out right.
func splitDays(start, end time.Time, loc *time.Location) []segment {
	if !end.After(start) {
		return []segment{{start.In(loc).Format("2006-01-02"), 0}}
	}
	var out []segment
	for cur := start.In(loc); cur.Before(end); {
		y, m, d := cur.Date()
		next := time.Date(y, m, d+1, 0, 0, 0, 0, loc)
		if next.After(end) {
			next = end
		}
		out = append(out, segment{cur.Format("2006-01-02"), next.Sub(cur)})
		cur = next.In(loc)
	}
	return out
}

func WriteDays(w io.Writer, rows []DayRow) error {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DAY\tHOURS\tBILLABLE")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Day, hours(r.Total), hours(r.Billable))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}
