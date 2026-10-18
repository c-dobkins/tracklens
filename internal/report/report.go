// Package report turns raw time entries into summaries, comparisons and
// data-quality findings, and renders them as tables, JSON or CSV.
package report

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/c-dobkins/p1/internal/toggl"
)

const noProject = "(no project)"

// Kind classifies a Finding.
type Kind string

// Kinds of data-quality finding.
const (
	KindRunning   Kind = "running"
	KindNoProject Kind = "no_project"
	KindOverlap   Kind = "overlap"
)

// Finding is one data-quality issue.
type Finding struct {
	Kind     Kind    `json:"kind"`
	EntryIDs []int64 `json:"entry_ids"`
	Message  string  `json:"message"`
}

// Options tunes the data-quality checks.
type Options struct {
	// OverlapTolerance ignores overlaps up to this long, which absorbs the
	// small clock slop between a timer stopping and the next one starting.
	OverlapTolerance time.Duration
}

// Row is one project's totals.
type Row struct {
	Project  string
	Total    time.Duration
	Billable time.Duration
}

// Summary is time per project plus the findings for the same entries.
type Summary struct {
	Rows     []Row
	Total    time.Duration
	Billable time.Duration
	Findings []Finding
}

// Summarize groups entries by project, largest first. now sizes running timers.
func Summarize(entries []toggl.Entry, names map[int64]string, now time.Time, opts Options) Summary {
	var s Summary
	byProject := map[string]*Row{}
	for _, e := range entries {
		d := e.Elapsed(now)
		if e.Running() {
			s.Findings = append(s.Findings, Finding{KindRunning, []int64{e.ID},
				fmt.Sprintf("entry %d is still running (%s so far)", e.ID, d.Round(time.Minute))})
		}
		if e.ProjectID == nil {
			s.Findings = append(s.Findings, Finding{KindNoProject, []int64{e.ID},
				fmt.Sprintf("entry %d %q has no project", e.ID, e.Description)})
		}
		name := projectName(e, names)
		r, ok := byProject[name]
		if !ok {
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
	slices.SortFunc(s.Rows, func(a, b Row) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), strings.Compare(a.Project, b.Project))
	})
	s.Findings = append(s.Findings, overlaps(entries, opts.OverlapTolerance)...)
	return s
}

// overlaps reports stopped entries that start before the furthest-ending
// earlier entry has finished. Tracking the furthest end (rather than only the
// previous entry) also catches one long entry that covers several short ones.
// Running timers are left out: their end is not known yet.
func overlaps(entries []toggl.Entry, tolerance time.Duration) []Finding {
	var stopped []toggl.Entry
	for _, e := range entries {
		if !e.Running() {
			stopped = append(stopped, e)
		}
	}
	slices.SortFunc(stopped, func(a, b toggl.Entry) int {
		return cmp.Or(a.Start.Compare(b.Start), cmp.Compare(a.ID, b.ID))
	})

	var noClock time.Time // stopped entries do not depend on the current time
	var out []Finding
	var latest *toggl.Entry
	for i := range stopped {
		cur := &stopped[i]
		if latest != nil {
			latestEnd := latest.End(noClock)
			// An entry wholly inside the earlier one overlaps by its own length.
			if by := min(latestEnd.Sub(cur.Start), cur.Elapsed(noClock)); by > tolerance {
				out = append(out, Finding{KindOverlap, []int64{latest.ID, cur.ID},
					fmt.Sprintf("entries %d and %d overlap by %s", latest.ID, cur.ID, by.Round(time.Second))})
			}
			if !cur.End(noClock).After(latestEnd) {
				continue
			}
		}
		latest = cur
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
	if e.ProjectName != "" {
		return e.ProjectName
	}
	return fmt.Sprintf("project %d", *e.ProjectID)
}

func percentOf(part, whole time.Duration) string {
	if whole <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", math.Round(100*float64(part)/float64(whole)))
}

// WriteTable renders the table followed by any findings.
func (s Summary) WriteTable(w io.Writer) error {
	t := newTable("PROJECT", "HOURS", "BILLABLE", "BILLABLE %")
	for _, r := range s.Rows {
		t.row(r.Project, fmtHours(r.Total), fmtHours(r.Billable), percentOf(r.Billable, r.Total))
	}
	t.row("TOTAL", fmtHours(s.Total), fmtHours(s.Billable), percentOf(s.Billable, s.Total))

	var extra strings.Builder
	if len(s.Findings) > 0 {
		extra.WriteString("\nChecks:\n")
		for _, f := range s.Findings {
			extra.WriteString(" - " + f.Message + "\n")
		}
	}
	return t.flush(w, extra.String())
}

// WriteJSON renders the summary and findings as JSON.
func (s Summary) WriteJSON(w io.Writer) error {
	type row struct {
		Project  string  `json:"project"`
		Hours    float64 `json:"hours"`
		Billable float64 `json:"billable_hours"`
	}
	out := struct {
		Rows     []row     `json:"rows"`
		Hours    float64   `json:"total_hours"`
		Billable float64   `json:"billable_hours"`
		Findings []Finding `json:"findings"`
	}{Rows: []row{}, Hours: roundHours(s.Total), Billable: roundHours(s.Billable), Findings: s.Findings}
	if out.Findings == nil {
		out.Findings = []Finding{}
	}
	for _, r := range s.Rows {
		out.Rows = append(out.Rows, row{r.Project, roundHours(r.Total), roundHours(r.Billable)})
	}
	return writeJSON(w, out)
}

// WriteCSV renders one row per project; findings are not included.
func (s Summary) WriteCSV(w io.Writer) error {
	rows := make([][]string, 0, len(s.Rows))
	for _, r := range s.Rows {
		rows = append(rows, []string{r.Project, fmtHours(r.Total), fmtHours(r.Billable)})
	}
	return writeCSV(w, []string{"project", "hours", "billable_hours"}, rows)
}
