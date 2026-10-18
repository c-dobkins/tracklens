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

// CompareRow is one project's hours in the current and previous window.
type CompareRow struct {
	Project  string
	Current  time.Duration
	Previous time.Duration
}

// Delta is current minus previous.
func (r CompareRow) Delta() time.Duration { return r.Current - r.Previous }

// Compare splits entries at cutoff (by start time) and totals each side per
// project: [cutoff, now] is "current", anything earlier is "previous". Rows are
// ordered by the size of the change so the biggest movers come first.
func Compare(entries []toggl.Entry, names map[int64]string, now, cutoff time.Time) []CompareRow {
	byProject := map[string]*CompareRow{}
	for _, e := range entries {
		d := time.Duration(e.Duration) * time.Second
		if e.Duration < 0 {
			d = now.Sub(e.Start)
		}
		name := projectName(e, names)
		r := byProject[name]
		if r == nil {
			r = &CompareRow{Project: name}
			byProject[name] = r
		}
		if e.Start.Before(cutoff) {
			r.Previous += d
		} else {
			r.Current += d
		}
	}
	rows := make([]CompareRow, 0, len(byProject))
	for _, r := range byProject {
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		ai, aj := abs(rows[i].Delta()), abs(rows[j].Delta())
		if ai != aj {
			return ai > aj
		}
		return rows[i].Project < rows[j].Project
	})
	return rows
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// WriteCompare renders the comparison table with a total row.
func WriteCompare(w io.Writer, rows []CompareRow) error {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PROJECT\tTHIS\tPREVIOUS\tCHANGE\tCHANGE %")
	var total CompareRow
	total.Project = "TOTAL"
	for _, r := range rows {
		writeCompareRow(tw, r)
		total.Current += r.Current
		total.Previous += r.Previous
	}
	writeCompareRow(tw, total)
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeCompareRow(w io.Writer, r CompareRow) {
	pct := "new"
	switch {
	case r.Previous > 0:
		pct = fmt.Sprintf("%+.0f%%", 100*float64(r.Delta())/float64(r.Previous))
	case r.Current == 0:
		pct = "-"
	}
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%+.2f\t%s\n", r.Project, hours(r.Current), hours(r.Previous), r.Delta().Hours(), pct)
}
