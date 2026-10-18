// Package report turns raw time entries into a summary plus data-quality findings.
package report

import (
	"fmt"
	"io"
	"sort"
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

// Summarize groups entries by project. now is used to size running timers.
func Summarize(entries []toggl.Entry, names map[int64]string, now time.Time) Summary {
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
		name := noProject
		if e.ProjectID != nil {
			if n, ok := names[*e.ProjectID]; ok {
				name = n
			} else {
				name = fmt.Sprintf("project %d", *e.ProjectID)
			}
		} else {
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
	s.Findings = append(s.Findings, overlaps(closed)...)
	return s
}

// overlaps reports pairs of closed entries whose time ranges intersect.
func overlaps(es []toggl.Entry) []string {
	sorted := append([]toggl.Entry(nil), es...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var out []string
	for i := 1; i < len(sorted); i++ {
		prev, cur := sorted[i-1], sorted[i]
		if prev.Stop != nil && cur.Start.Before(*prev.Stop) {
			out = append(out, fmt.Sprintf("entries %d and %d overlap by %s", prev.ID, cur.ID, prev.Stop.Sub(cur.Start).Round(time.Second)))
		}
	}
	return out
}

func hours(d time.Duration) string { return fmt.Sprintf("%.2f", d.Hours()) }

func (s Summary) Write(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROJECT\tHOURS\tBILLABLE\tBILLABLE %")
	for _, r := range s.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Project, hours(r.Total), hours(r.Billable), pct(r.Billable, r.Total))
	}
	fmt.Fprintf(tw, "TOTAL\t%s\t%s\t%s\n", hours(s.Total), hours(s.Billable), pct(s.Billable, s.Total))
	tw.Flush()
	if len(s.Findings) > 0 {
		fmt.Fprintln(w, "\nChecks:")
		for _, f := range s.Findings {
			fmt.Fprintln(w, " -", f)
		}
	}
}

func pct(a, b time.Duration) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(a)/float64(b))
}
