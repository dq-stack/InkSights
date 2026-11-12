// Package history is the append-only reading log built from timer.model
// snapshots (/mnt/us/reading-stats/snapshots.jsonl).
//
// The Kindle only keeps a cumulative total per book (no dates), and that
// total is lost when a book is re-sent or the device is wiped. So each
// launch compares current totals with the last ones logged and writes one
// row per book that gained time:
//
//	{"ts": "2026-10-02T09:15:00", "key": "<sdr path>", "title": "...",
//	 "total_ms": 8004000, "percent": 0.667, "words": 64392,
//	 "gained_ms": 120000, "alloc": {"2026-10-01": 120000}}
//
// "alloc" says which days the gained time belongs to. Stats are computed
// only from rows' gained/alloc values, never from current totals, so rows
// can be merged across backups (dedupe on ts+key) and lost sidecars cost
// nothing.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/clock"
)

// Undated holds reading from before tracking started (the first launch's totals).
const Undated = "undated"

// Row is one line of the reading log. Field order matches the Python
// version's output.
type Row struct {
	Ts       string           `json:"ts"`
	Key      string           `json:"key"`
	Title    string           `json:"title"`
	TotalMs  int64            `json:"total_ms"`
	Percent  *float64         `json:"percent"`
	Words    *int64           `json:"words"`
	GainedMs int64            `json:"gained_ms"`
	Alloc    map[string]int64 `json:"alloc"`
}

// Load reads the log; unreadable lines (e.g. a torn final line from a
// power-off) are skipped.
func Load(path string) []Row {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var rows []Row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Row
		if json.Unmarshal(line, &r) != nil || r.Key == "" {
			continue
		}
		rows = append(rows, r)
	}
	return rows
}

// lastTotals is the latest logged row per sidecar key.
func lastTotals(rows []Row) map[string]Row {
	m := map[string]Row{}
	for _, r := range rows {
		m[r.Key] = r
	}
	return m
}

// Weights gives awake seconds per day for a book's reading (see activity).
type Weights interface {
	DayWeights(since *time.Time, until time.Time, key string) map[string]float64
}

// Allocate splits gained time across days.
//
// weights (awake seconds per day) is used first: it says when the Kindle
// was actually in use. Without it, the book's page.history timestamps
// inside (since, until] are used, but those only mark jumps (links, Go To),
// so they're a poor guide. With neither: the first-ever (baseline) reading
// is undated, any later gain goes to the day of until.
func Allocate(gained int64, historyMs []int64, since *time.Time, until time.Time, baseline bool,
	weights map[string]float64) map[string]int64 {
	days := map[string]int64{}
	for d, w := range weights {
		if r := int64(math.RoundToEven(w)); r > 0 {
			days[d] = r
		}
	}
	if len(days) == 0 {
		for _, ms := range historyMs {
			t := clock.FromMs(ms)
			if t.After(until) || (since != nil && !t.After(*since)) {
				continue
			}
			days[clock.Day(t)]++
		}
	}
	if len(days) == 0 {
		key := clock.Day(until)
		if baseline {
			key = Undated
		}
		return map[string]int64{key: gained}
	}
	var n int64
	keys := make([]string, 0, len(days))
	for d, c := range days {
		n += c
		keys = append(keys, d)
	}
	sort.Strings(keys)
	alloc := map[string]int64{}
	var assigned int64
	for i, d := range keys {
		part := gained * days[d] / n
		if i == len(keys)-1 {
			part = gained - assigned
		}
		alloc[d] = part
		assigned += part
	}
	return alloc
}

// BuildRows returns the rows to append for this launch (books without
// timer data are skipped).
func BuildRows(bks []books.Book, rows []Row, now time.Time, w Weights) []Row {
	prev := lastTotals(rows)
	firstRun := len(rows) == 0
	var lastTs *time.Time
	for _, r := range rows {
		if t, ok := clock.Parse(r.Ts); ok && (lastTs == nil || t.After(*lastTs)) {
			t := t
			lastTs = &t
		}
	}
	var out []Row
	for _, b := range bks {
		total := b.TotalMs
		if total == 0 {
			continue
		}
		p, seen := prev[b.Key]
		if seen && total == p.TotalMs {
			continue
		}
		var gained int64
		var since *time.Time
		if !seen || total < p.TotalMs {
			// new book, or sidecar was reset (re-send): everything in it is new reading
			gained = total
			if !firstRun {
				since = lastTs
			}
		} else {
			gained = total - p.TotalMs
			if t, ok := clock.Parse(p.Ts); ok {
				since = &t
			}
		}
		if gained <= 0 {
			continue
		}
		var weights map[string]float64
		if w != nil {
			weights = w.DayWeights(since, now, b.Key)
		}
		var words *int64
		if b.HasWords {
			v := b.Words
			words = &v
		}
		out = append(out, Row{
			Ts:       now.Format(clock.Layout),
			Key:      b.Key,
			Title:    b.Title,
			TotalMs:  total,
			Percent:  b.Percent,
			Words:    words,
			GainedMs: gained,
			Alloc:    Allocate(gained, b.HistoryMs, since, now, firstRun && !seen, weights),
		})
	}
	return out
}

// Record appends this launch's rows to the log (synced to disk) and returns
// the full log.
func Record(path string, bks []books.Book, now time.Time, w Weights) ([]Row, error) {
	rows := Load(path)
	add := BuildRows(bks, rows, now, w)
	if len(add) == 0 {
		return rows, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return rows, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return rows, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, r := range add {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return rows, err
		}
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return rows, err
	}
	f.Sync()
	if err := f.Close(); err != nil {
		return rows, err
	}
	return append(rows, add...), nil
}
