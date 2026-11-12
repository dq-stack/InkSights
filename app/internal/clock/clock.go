// Package clock handles time the way the reading log stores it: plain local
// wall-clock times with no zone ("2026-11-08T12:41:45"), as the Kindle's
// clock shows them.
//
// All times are kept as wall-clock readings labelled UTC, so arithmetic and
// day boundaries work on the clock face exactly like the original Python
// version's naive datetimes (no zone or DST conversion ever happens).
package clock

import "time"

// Layout is the reading log's timestamp format.
const Layout = "2006-01-02T15:04:05"

// DayLayout is the format of day keys ("2026-11-08").
const DayLayout = "2006-01-02"

// Open stands for "still going at the end of the log".
var Open = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// Wall turns a real instant into its local wall-clock reading.
func Wall(t time.Time) time.Time {
	l := t.In(time.Local)
	return time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), time.UTC)
}

// Now is the local wall clock now, to the second.
func Now() time.Time { return Wall(time.Now()).Truncate(time.Second) }

// FromMs is the local wall-clock time of a millisecond timestamp, to the second.
func FromMs(ms int64) time.Time { return Wall(time.UnixMilli(ms)).Truncate(time.Second) }

// Parse reads a log timestamp (only the first 19 characters count).
func Parse(s string) (time.Time, bool) {
	if len(s) > 19 {
		s = s[:19]
	}
	t, err := time.Parse(Layout, s)
	return t, err == nil
}

// Day is the day key of a wall-clock time.
func Day(t time.Time) string { return t.Format(DayLayout) }

// Date is midnight at the start of t's day.
func Date(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
