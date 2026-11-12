// Package stats turns the reading log into the numbers the card shows.
package stats

import (
	"sort"
	"strings"
	"time"

	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/clock"
	"github.com/dq-stack/InkSights/app/internal/history"
)

const (
	StreakMinMs     = 5 * 60 * 1000 // a day counts toward a streak at 5+ minutes
	FinishedPercent = 0.98
	HeatmapWeeks    = 26
)

// PerDay sums each day's allocated reading time.
func PerDay(rows []history.Row) map[string]int64 {
	days := map[string]int64{}
	for _, r := range rows {
		for d, ms := range r.Alloc {
			days[d] += ms
		}
	}
	return days
}

// Streaks returns (current, best) runs of consecutive days at or above
// StreakMinMs. The current streak survives until the end of today, so it
// still counts yesterday's run if you haven't read yet today.
func Streaks(days map[string]int64, today time.Time) (int, int) {
	var active []time.Time
	for d, ms := range days {
		if d == history.Undated || ms < StreakMinMs {
			continue
		}
		if t, err := time.Parse(clock.DayLayout, d); err == nil {
			active = append(active, t)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Before(active[j]) })
	best, run := 0, 0
	for i, d := range active {
		if i > 0 && d.Sub(active[i-1]) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
	}
	current := 0
	if n := len(active); n > 0 && !active[n-1].Before(today.AddDate(0, 0, -1)) {
		current = 1
		for i := n - 1; i > 0; i-- {
			if active[i].Sub(active[i-1]) != 24*time.Hour {
				break
			}
			current++
		}
	}
	return current, best
}

// HeatLevel is a day's heatmap shade, 0 (none) to 4 (an hour or more).
func HeatLevel(ms int64) int {
	m := float64(ms) / 60000
	switch {
	case m <= 0:
		return 0
	case m < 15:
		return 1
	case m < 30:
		return 2
	case m < 60:
		return 3
	}
	return 4
}

// Heatmap is a weeks x 7 grid of levels 0-4, columns oldest to newest, rows
// Monday to Sunday. Days after today are -1.
func Heatmap(days map[string]int64, today time.Time, weeks int) [][]int {
	wd := (int(today.Weekday()) + 6) % 7 // Monday = 0
	end := today.AddDate(0, 0, 6-wd)     // Sunday of this week
	start := end.AddDate(0, 0, -(weeks*7 - 1))
	grid := make([][]int, weeks)
	for w := 0; w < weeks; w++ {
		col := make([]int, 7)
		for dow := 0; dow < 7; dow++ {
			d := start.AddDate(0, 0, w*7+dow)
			if d.After(today) {
				col[dow] = -1
			} else {
				col[dow] = HeatLevel(days[clock.Day(d)])
			}
		}
		grid[w] = col
	}
	return grid
}

// TopBook is one "Most read" line.
type TopBook struct {
	Title   string
	Ms      int64
	Percent *float64
}

// Stats is everything the card shows.
type Stats struct {
	TodayMs, LifetimeMs, YearMs int64
	Streak, BestStreak          int
	YearBooks, Books, Finished  int
	WPM                         *int
	MonthlyMs                   [12]int64
	Top                         []TopBook
	Heatmap                     [][]int
	Since                       string // first dated day, "" if none
}

type bookAgg struct {
	title   string
	ms      int64
	percent *float64
	years   map[string]bool
	latest  map[string][2]int64 // key -> (words, total ms)
	now     *float64
}

// Compute builds the stats from the log. current is this launch's collected
// books: their progress wins over the log's, which only gains a row when
// reading time goes up (so "Mark as Read" alone would never reach the card).
func Compute(rows []history.Row, today time.Time, current []books.Book) Stats {
	days := PerDay(rows)
	year := today.Format("2006")

	var order []*bookAgg
	byTitle := map[string]*bookAgg{}
	for _, r := range rows {
		b := byTitle[r.Title]
		if b == nil {
			b = &bookAgg{title: r.Title, years: map[string]bool{}, latest: map[string][2]int64{}}
			byTitle[r.Title] = b
			order = append(order, b)
		}
		b.ms += r.GainedMs
		if r.Percent != nil {
			p := *r.Percent
			b.percent = &p
		}
		for d := range r.Alloc {
			if d != history.Undated && len(d) >= 4 {
				b.years[d[:4]] = true
			}
		}
		var words int64
		if r.Words != nil {
			words = *r.Words
		}
		b.latest[r.Key] = [2]int64{words, r.TotalMs}
	}

	for _, c := range current {
		if c.Percent == nil {
			continue
		}
		b := byTitle[c.Title]
		if b == nil {
			continue
		}
		p := *c.Percent
		if b.now != nil && *b.now > p {
			p = *b.now
		}
		b.percent = &p
		b.now = &p
	}

	var words, timerMs int64
	for _, b := range order {
		for _, wt := range b.latest {
			words += wt[0]
			timerMs += wt[1]
		}
	}

	top := make([]*bookAgg, len(order))
	copy(top, order)
	sort.SliceStable(top, func(i, j int) bool { return top[i].ms > top[j].ms })

	var s Stats
	for d, ms := range days {
		if strings.HasPrefix(d, year+"-") {
			var m int
			if t, err := time.Parse(clock.DayLayout, d); err == nil {
				m = int(t.Month()) - 1
				s.MonthlyMs[m] += ms
			}
		}
		s.LifetimeMs += ms
	}
	for _, ms := range s.MonthlyMs {
		s.YearMs += ms
	}
	s.TodayMs = days[clock.Day(today)]
	s.Streak, s.BestStreak = Streaks(days, today)
	s.Books = len(order)
	for _, b := range order {
		if b.years[year] {
			s.YearBooks++
		}
		if b.percent != nil && *b.percent >= FinishedPercent {
			s.Finished++
		}
	}
	if timerMs > 0 {
		w := int(float64(words) / (float64(timerMs) / 60000))
		s.WPM = &w
	}
	for _, b := range top {
		s.Top = append(s.Top, TopBook{Title: b.title, Ms: b.ms, Percent: b.percent})
	}
	s.Heatmap = Heatmap(days, today, HeatmapWeeks)
	for d := range days {
		if d == history.Undated {
			continue
		}
		if s.Since == "" || d < s.Since {
			s.Since = d
		}
	}
	return s
}
