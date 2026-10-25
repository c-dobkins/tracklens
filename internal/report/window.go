package report

import (
	"time"

	"github.com/c-dobkins/tracklens/internal/toggl"
)

// compareMargin is how far before the previous period entries are loaded
// for a comparison, so time from an entry that started earlier but ran into
// that period is counted. Entries longer than this are rare enough to ignore.
const compareMargin = 24 * time.Hour

// Window is the start-time range of entries to load for a report on the last
// days days ending at now. For a comparison it also covers the previous period
// plus a margin. The end reaches toggl.ClockSkew past now so entries from a
// device whose clock runs fast are included.
func Window(now time.Time, days int, compare bool) (from, to time.Time) {
	from = now.AddDate(0, 0, -days)
	if compare {
		from = now.AddDate(0, 0, -2*days).Add(-compareMargin)
	}
	return from, now.Add(toggl.ClockSkew)
}

// DayWindow is the load range for a per-day report covering the last days
// calendar days in loc, today included: from local midnight days-1 days ago,
// so "7 days" means seven whole days rather than eight with a partial first.
func DayWindow(now time.Time, days int, loc *time.Location) (from, to time.Time) {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d-(days-1), 0, 0, 0, 0, loc), now.Add(toggl.ClockSkew)
}

// Periods are the bounds Compare needs for a comparison of the last days
// days ending at now with the days before them.
func Periods(now time.Time, days int) (from, cutoff time.Time) {
	return now.AddDate(0, 0, -2*days), now.AddDate(0, 0, -days)
}
