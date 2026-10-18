package report

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"time"
)

type jsonRow struct {
	Project  string  `json:"project"`
	Hours    float64 `json:"hours"`
	Billable float64 `json:"billable_hours"`
}

// WriteJSON emits the summary as machine-readable JSON.
func (s Summary) WriteJSON(w io.Writer) error {
	out := struct {
		Rows     []jsonRow `json:"rows"`
		Hours    float64   `json:"total_hours"`
		Billable float64   `json:"billable_hours"`
		Findings []string  `json:"findings"`
	}{Rows: []jsonRow{}, Hours: h(s.Total), Billable: h(s.Billable), Findings: s.Findings}
	if out.Findings == nil {
		out.Findings = []string{}
	}
	for _, r := range s.Rows {
		out.Rows = append(out.Rows, jsonRow{r.Project, h(r.Total), h(r.Billable)})
	}
	return writeJSON(w, out)
}

// WriteCSV emits one row per project.
func (s Summary) WriteCSV(w io.Writer) error {
	rows := make([][]string, 0, len(s.Rows))
	for _, r := range s.Rows {
		rows = append(rows, []string{r.Project, f2(r.Total), f2(r.Billable)})
	}
	return writeCSV(w, []string{"project", "hours", "billable_hours"}, rows)
}

// WriteDaysJSON emits the per-day totals as JSON.
func WriteDaysJSON(w io.Writer, rows []DayRow) error {
	type day struct {
		Day      string  `json:"day"`
		Hours    float64 `json:"hours"`
		Billable float64 `json:"billable_hours"`
	}
	out := make([]day, 0, len(rows))
	for _, r := range rows {
		out = append(out, day{r.Day, h(r.Total), h(r.Billable)})
	}
	return writeJSON(w, map[string]any{"days": out})
}

// WriteDaysCSV emits the per-day totals as CSV.
func WriteDaysCSV(w io.Writer, rows []DayRow) error {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{r.Day, f2(r.Total), f2(r.Billable)})
	}
	return writeCSV(w, []string{"day", "hours", "billable_hours"}, out)
}

// WriteCompareJSON emits the comparison as JSON. change_percent is null when
// the project has no previous time to compare against.
func WriteCompareJSON(w io.Writer, rows []CompareRow) error {
	type row struct {
		Project  string   `json:"project"`
		Current  float64  `json:"current_hours"`
		Previous float64  `json:"previous_hours"`
		Change   float64  `json:"change_hours"`
		Percent  *float64 `json:"change_percent"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		x := row{r.Project, h(r.Current), h(r.Previous), h(r.Delta()), nil}
		if r.Previous > 0 {
			p := math.Round(1000*float64(r.Delta())/float64(r.Previous)) / 10
			x.Percent = &p
		}
		out = append(out, x)
	}
	return writeJSON(w, map[string]any{"rows": out})
}

// WriteCompareCSV emits the comparison as CSV; change_percent is empty when
// there is no previous time.
func WriteCompareCSV(w io.Writer, rows []CompareRow) error {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		pct := ""
		if r.Previous > 0 {
			pct = strconv.FormatFloat(math.Round(1000*float64(r.Delta())/float64(r.Previous))/10, 'f', 1, 64)
		}
		out = append(out, []string{r.Project, f2(r.Current), f2(r.Previous), strconv.FormatFloat(h(r.Delta()), 'f', 2, 64), pct})
	}
	return writeCSV(w, []string{"project", "current_hours", "previous_hours", "change_hours", "change_percent"}, out)
}

// h is hours rounded to two decimals, the precision used in every output.
func h(d time.Duration) float64 { return math.Round(d.Hours()*100) / 100 }

func f2(d time.Duration) string { return strconv.FormatFloat(h(d), 'f', 2, 64) }

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeCSV(w io.Writer, header []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write(r); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
