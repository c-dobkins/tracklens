package report

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
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
	}{Rows: []jsonRow{}, Hours: s.Total.Hours(), Billable: s.Billable.Hours(), Findings: s.Findings}
	if out.Findings == nil {
		out.Findings = []string{}
	}
	for _, r := range s.Rows {
		out.Rows = append(out.Rows, jsonRow{r.Project, r.Total.Hours(), r.Billable.Hours()})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteCSV emits one row per project.
func (s Summary) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"project", "hours", "billable_hours"})
	for _, r := range s.Rows {
		cw.Write([]string{r.Project, strconv.FormatFloat(r.Total.Hours(), 'f', 2, 64), strconv.FormatFloat(r.Billable.Hours(), 'f', 2, 64)})
	}
	cw.Flush()
	return cw.Error()
}
