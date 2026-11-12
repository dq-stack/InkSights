package stats_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/clock"
	"github.com/dq-stack/InkSights/app/internal/history"
	"github.com/dq-stack/InkSights/app/internal/stats"
)

const min = 60000

func day(s string) time.Time {
	t, _ := clock.Parse(s + "T00:00:00")
	return t
}

func rows(perDay map[string]int64) []history.Row {
	var out []history.Row
	p := 0.99
	for d, ms := range perDay {
		out = append(out, history.Row{Ts: d + "T23:00:00", Key: "k", Title: "Book",
			Percent: &p, GainedMs: ms, Alloc: map[string]int64{d: ms}})
	}
	return out
}

func TestStreaks(t *testing.T) {
	s := stats.Compute(rows(map[string]int64{"2026-09-01": 10 * min, "2026-09-02": 10 * min,
		"2026-09-03": 10 * min, "2026-10-01": 10 * min, "2026-10-02": 2 * min, "2026-10-03": 6 * min}),
		day("2026-10-03"), nil)
	if s.Streak != 1 || s.BestStreak != 3 {
		t.Fatalf("streak %d best %d", s.Streak, s.BestStreak)
	}
}

func TestStreakSurvivesUntilEndOfToday(t *testing.T) {
	r := rows(map[string]int64{"2026-10-01": 10 * min, "2026-10-02": 10 * min})
	if s := stats.Compute(r, day("2026-10-03"), nil); s.Streak != 2 {
		t.Fatal(s.Streak)
	}
	if s := stats.Compute(r, day("2026-10-04"), nil); s.Streak != 0 {
		t.Fatal(s.Streak)
	}
}

func TestHeatmapShapeAndLevels(t *testing.T) {
	grid := stats.Compute(rows(map[string]int64{"2026-10-01": 90 * min, "2026-09-30": 10 * min}),
		day("2026-10-02"), nil).Heatmap // a Friday
	if len(grid) != 26 || len(grid[0]) != 7 {
		t.Fatal("shape")
	}
	// this week, Mon..Sun: Wed 30 Sep = 10 min, Thu 1 Oct = 90 min, Fri = today, weekend = future
	if want := []int{0, 0, 1, 4, 0, -1, -1}; !reflect.DeepEqual(grid[25], want) {
		t.Fatalf("got %v", grid[25])
	}
}

func TestMonthlyYearAndFinished(t *testing.T) {
	s := stats.Compute(rows(map[string]int64{"2026-09-01": 60 * min, "2026-10-01": 30 * min,
		"2025-12-31": 5 * min}), day("2026-10-02"), nil)
	if s.MonthlyMs[8] != 60*min || s.YearMs != 90*min || s.LifetimeMs != 95*min || s.Finished != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestMarkAsReadReachesTheCardWithoutNewReading(t *testing.T) {
	p := 0.7251717
	r := []history.Row{{Ts: "2026-10-14T02:07:00", Key: "Badawi/African.sdr",
		Title: "An African History of Africa", TotalMs: 3600000, Percent: &p, GainedMs: 3600000,
		Alloc: map[string]int64{"2026-10-14": 3600000}}}
	done := 1.0
	cur := []books.Book{{Key: "Badawi/African.sdr", Title: "An African History of Africa", Percent: &done}}
	before := stats.Compute(r, day("2026-10-18"), nil)
	after := stats.Compute(r, day("2026-10-18"), cur)
	if *before.Top[0].Percent != p || *after.Top[0].Percent != 1 || after.Finished != before.Finished+1 {
		t.Fatal("mark as read not applied")
	}
}

func TestBooksNotInTheLogAreNotAdded(t *testing.T) {
	half := 0.5
	cur := []books.Book{{Key: "x.sdr", Title: "Never opened", Percent: &half}}
	if stats.Compute(nil, day("2026-10-18"), cur).Books != 0 {
		t.Fatal("added")
	}
}
