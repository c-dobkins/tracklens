package report

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/c-dobkins/p1/internal/toggl"
)

// CompareRow is one project's time in the current and previous window.
type CompareRow struct {
	Project  string
	Current  time.Duration
	Previous time.Duration
}

// Delta is current minus previous.
func (r CompareRow) Delta() time.Duration { return r.Current - r.Previous }

// Comparison is a per-project comparison, biggest change first.
type Comparison []CompareRow

// Compare splits entries at cutoff by start time: entries starting at or after
// cutoff are "current", earlier ones "previous".
func Compare(entries []toggl.Entry, names map[int64]string, now, cutoff time.Time) Comparison {
	byProject := map[string]*CompareRow{}
	for _, e := range entries {
		name := projectName(e, names)
		r, ok := byProject[name]
		if !ok {
			r = &CompareRow{Project: name}
			byProject[name] = r
		}
		if e.Start.Before(cutoff) {
			r.Previous += e.Elapsed(now)
		} else {
			r.Current += e.Elapsed(now)
		}
	}
	rows := make(Comparison, 0, len(byProject))
	for _, r := range byProject {
		rows = append(rows, *r)
	}
	slices.SortFunc(rows, func(a, b CompareRow) int {
		return cmp.Or(cmp.Compare(b.Delta().Abs(), a.Delta().Abs()), strings.Compare(a.Project, b.Project))
	})
	return rows
}

func (c Comparison) total() CompareRow {
	t := CompareRow{Project: "TOTAL"}
	for _, r := range c {
		t.Current += r.Current
		t.Previous += r.Previous
	}
	return t
}

// WriteTable renders the comparison with a total row.
func (c Comparison) WriteTable(w io.Writer) error {
	t := newTable("PROJECT", "THIS", "PREVIOUS", "CHANGE", "CHANGE %")
	row := func(r CompareRow) {
		pct := "-"
		if p, ok := changePercent(r.Current, r.Previous); ok {
			pct = fmt.Sprintf("%+.0f%%", math.Round(p)) // half away from zero, matching JSON/CSV
		} else if r.Current > 0 {
			pct = "new"
		}
		t.row(r.Project, fmtHours(r.Current), fmtHours(r.Previous), fmt.Sprintf("%+.2f", roundHours(r.Delta())), pct)
	}
	for _, r := range c {
		row(r)
	}
	row(c.total())
	return t.flush(w, "")
}

// WriteJSON renders the comparison; change_percent is null when there is no
// previous time to compare against.
func (c Comparison) WriteJSON(w io.Writer) error {
	type row struct {
		Project  string   `json:"project"`
		Current  float64  `json:"current_hours"`
		Previous float64  `json:"previous_hours"`
		Change   float64  `json:"change_hours"`
		Percent  *float64 `json:"change_percent"`
	}
	out := make([]row, 0, len(c))
	for _, r := range c {
		x := row{r.Project, roundHours(r.Current), roundHours(r.Previous), roundHours(r.Delta()), nil}
		if p, ok := changePercent(r.Current, r.Previous); ok {
			x.Percent = &p
		}
		out = append(out, x)
	}
	return writeJSON(w, struct {
		Rows []row `json:"rows"`
	}{out})
}

// WriteCSV renders the comparison; change_percent is empty when there is no
// previous time.
func (c Comparison) WriteCSV(w io.Writer) error {
	rows := make([][]string, 0, len(c))
	for _, r := range c {
		pct := ""
		if p, ok := changePercent(r.Current, r.Previous); ok {
			pct = strconv.FormatFloat(p, 'f', 1, 64)
		}
		rows = append(rows, []string{r.Project, fmtHours(r.Current), fmtHours(r.Previous), fmtHours(r.Delta()), pct})
	}
	return writeCSV(w, []string{"project", "current_hours", "previous_hours", "change_hours", "change_percent"}, rows)
}
