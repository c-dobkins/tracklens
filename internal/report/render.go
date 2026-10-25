package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Format is an output format accepted by Render.
type Format string

// Supported output formats.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
)

// ParseFormat validates a user-supplied format name.
func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case FormatTable, FormatJSON, FormatCSV:
		return f, nil
	}
	return "", fmt.Errorf("unknown format %q (want table, json or csv)", s)
}

// Renderer is implemented by every report view.
type Renderer interface {
	WriteTable(w io.Writer) error
	WriteJSON(w io.Writer) error
	WriteCSV(w io.Writer) error
}

// Render writes r to w in the given format.
func Render(w io.Writer, f Format, r Renderer) error {
	switch f {
	case FormatJSON:
		return r.WriteJSON(w)
	case FormatCSV:
		return r.WriteCSV(w)
	case FormatTable:
		return r.WriteTable(w)
	}
	return fmt.Errorf("unknown format %q", f)
}

// roundHours is d in hours, rounded to the two decimals used in every output.
func roundHours(d time.Duration) float64 { return math.Round(d.Hours()*100) / 100 }

func fmtHours(d time.Duration) string { return strconv.FormatFloat(roundHours(d), 'f', 2, 64) }

// changePercent is the unrounded change from prev to cur in percent, and
// false when there is no previous value to compare against. Each output
// rounds it once, to its own precision.
func changePercent(cur, prev time.Duration) (float64, bool) {
	if prev <= 0 {
		return 0, false
	}
	return 100 * float64(cur-prev) / float64(prev), true
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// table builds tab-aligned output in memory so a failing writer is reported
// once, as an error, rather than after a partial write.
type table struct {
	b  strings.Builder
	tw *tabwriter.Writer
}

func newTable(header ...string) *table {
	t := &table{}
	t.tw = tabwriter.NewWriter(&t.b, 0, 0, 2, ' ', 0)
	t.row(header...)
	return t
}

func (t *table) row(cells ...string) {
	_, _ = io.WriteString(t.tw, strings.Join(cells, "\t")+"\n") // writes to a strings.Builder cannot fail
}

// flush finishes the table, appends extra text, and writes everything to w.
func (t *table) flush(w io.Writer, extra string) error {
	if err := t.tw.Flush(); err != nil {
		return err
	}
	t.b.WriteString(extra)
	_, err := io.WriteString(w, t.b.String())
	return err
}

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
	if err := cw.WriteAll(rows); err != nil { // WriteAll flushes
		return err
	}
	return cw.Error()
}
