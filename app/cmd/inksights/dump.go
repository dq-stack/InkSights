package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/dq-stack/InkSights/app/internal/activity"
	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/clock"
	"github.com/dq-stack/InkSights/app/internal/history"
	"github.com/dq-stack/InkSights/app/internal/stats"
)

// dump prints what one launch would compute, as JSON, without writing
// anything: the collected books, the rows it would log and the stats.
// Used to check this version against the original Python one.
func dump(args []string) int {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	docs := fs.String("documents", "/mnt/us/documents", "documents folder")
	ccdb := fs.String("ccdb", "/var/local/cc.db", "library database")
	data := fs.String("data", "/mnt/us/reading-stats", "InkSights data folder")
	cslog := fs.String("coversleep", "/mnt/us/extensions/custom-screensaver/custom_ss.log", "CoverSleep log")
	desk := fs.String("desk", "/mnt/us/desk/desk.log", "Desk Buddy log")
	nowS := fs.String("now", "", "pretend it's this local time (2006-01-02T15:04:05)")
	fs.Parse(args)

	now := clock.Now()
	if *nowS != "" {
		t, ok := clock.Parse(*nowS)
		if !ok {
			fmt.Fprintln(os.Stderr, "bad -now")
			return 2
		}
		now = t
	}
	bks := books.Collect(*docs, *ccdb)
	rows := history.Load(*data + "/snapshots.jsonl")
	act := activity.New(*data+"/activity.log", *cslog, *desk)
	add := history.BuildRows(bks, rows, now, act)
	s := stats.Compute(append(rows, add...), clock.Date(now), bks)

	type jbook struct {
		Key       string   `json:"key"`
		Title     string   `json:"title"`
		TotalMs   *int64   `json:"total_ms"`
		Words     *int64   `json:"words"`
		Percent   *float64 `json:"percent"`
		History   []string `json:"history"`
		Sidecars  []string `json:"sidecars"`
		NumErrors int      `json:"n_errors"`
	}
	var jb []jbook
	for _, b := range bks {
		j := jbook{Key: b.Key, Title: b.Title, Percent: b.Percent, Sidecars: b.Sidecars, NumErrors: len(b.Errors)}
		if b.TotalMs != 0 || b.HasWords {
			t := b.TotalMs
			j.TotalMs = &t
		}
		if b.HasWords {
			w := b.Words
			j.Words = &w
		}
		for _, ms := range b.HistoryMs {
			j.History = append(j.History, clock.FromMs(ms).Format(clock.Layout))
		}
		if j.History == nil {
			j.History = []string{}
		}
		if j.Sidecars == nil {
			j.Sidecars = []string{}
		}
		jb = append(jb, j)
	}
	sort.Slice(jb, func(i, k int) bool { return jb[i].Key < jb[k].Key })

	heat := s.Heatmap
	out := map[string]any{
		"books":    jb,
		"new_rows": add,
		"stats": map[string]any{
			"today_ms": s.TodayMs, "lifetime_ms": s.LifetimeMs, "streak": s.Streak,
			"best_streak": s.BestStreak, "year_ms": s.YearMs, "year_books": s.YearBooks,
			"books": s.Books, "finished": s.Finished, "wpm": s.WPM, "monthly_ms": s.MonthlyMs,
			"top": s.Top, "heatmap": heat, "since": s.Since,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	enc.SetEscapeHTML(false)
	enc.Encode(out)
	_ = time.Now
	return 0
}
