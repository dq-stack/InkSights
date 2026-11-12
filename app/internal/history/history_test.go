package history_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dq-stack/InkSights/app/internal/activity"
	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/clock"
	"github.com/dq-stack/InkSights/app/internal/history"
	"github.com/dq-stack/InkSights/app/internal/stats"
)

const min = 60000

func TestMain(m *testing.M) {
	time.Local = time.UTC // so page-history ms map back to the same wall clock
	os.Exit(m.Run())
}

func dt(s string) time.Time {
	t, ok := clock.Parse(s)
	if !ok {
		panic(s)
	}
	return t
}

func ms(s string) int64 { return dt(s).UnixMilli() }

func book(key string, total int64, hist ...string) books.Book {
	p := 0.5
	b := books.Book{Key: key, Title: key, TotalMs: total, Words: 1000, HasWords: true, Percent: &p}
	for _, h := range hist {
		b.HistoryMs = append(b.HistoryMs, ms(h))
	}
	return b
}

type logFile string

func newLog(t *testing.T) logFile { return logFile(filepath.Join(t.TempDir(), "snapshots.jsonl")) }

func (l logFile) at(t *testing.T, when string, bks ...books.Book) []history.Row {
	rows, err := history.Record(string(l), bks, dt(when), nil)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFirstRunWithoutHistoryIsUndated(t *testing.T) {
	rows := newLog(t).at(t, "2026-10-02T09:00:00", book("a", 30*min))
	eq(t, rows[0].Alloc, map[string]int64{"undated": 30 * min})
}

func TestFirstRunSpreadsByPageHistory(t *testing.T) {
	rows := newLog(t).at(t, "2026-10-02T09:00:00", book("a", 90*min,
		"2026-09-30T20:00:00", "2026-10-01T20:00:00", "2026-10-01T21:00:00"))
	eq(t, rows[0].Alloc, map[string]int64{"2026-09-30": 30 * min, "2026-10-01": 60 * min})
}

func TestUnchangedTotalWritesNothing(t *testing.T) {
	l := newLog(t)
	l.at(t, "2026-10-02T09:00:00", book("a", 30*min))
	eq(t, len(l.at(t, "2026-10-03T09:00:00", book("a", 30*min))), 1)
}

func TestNormalDeltaGoesToLaunchDay(t *testing.T) {
	l := newLog(t)
	l.at(t, "2026-10-02T09:00:00", book("a", 30*min))
	rows := l.at(t, "2026-10-03T09:00:00", book("a", 50*min))
	eq(t, rows[len(rows)-1].GainedMs, int64(20*min))
	eq(t, rows[len(rows)-1].Alloc, map[string]int64{"2026-10-03": 20 * min})
}

func TestMultiDayGapUsesHistoryInsideWindowOnly(t *testing.T) {
	l := newLog(t)
	l.at(t, "2026-10-02T09:00:00", book("a", 30*min, "2026-10-01T20:00:00"))
	rows := l.at(t, "2026-10-06T09:00:00", book("a", 70*min,
		"2026-10-01T20:00:00", "2026-10-03T20:00:00", "2026-10-05T20:00:00"))
	eq(t, rows[len(rows)-1].Alloc, map[string]int64{"2026-10-03": 20 * min, "2026-10-05": 20 * min})
}

func TestResetSidecarCountsNewTotalWithoutLosingOld(t *testing.T) {
	l := newLog(t)
	l.at(t, "2026-10-02T09:00:00", book("a", 60*min))
	rows := l.at(t, "2026-10-04T09:00:00", book("a", 10*min))
	eq(t, rows[len(rows)-1].GainedMs, int64(10*min))
	eq(t, stats.Compute(rows, dt("2026-10-04T00:00:00"), nil).LifetimeMs, int64(70*min))
}

func TestNewBookAfterInstallIsDated(t *testing.T) {
	l := newLog(t)
	l.at(t, "2026-10-02T09:00:00", book("a", 60*min))
	rows := l.at(t, "2026-10-04T09:00:00", book("a", 60*min), book("b", 15*min))
	eq(t, rows[len(rows)-1].Alloc, map[string]int64{"2026-10-04": 15 * min})
}

func TestBooksWithoutTimerSkippedAndTornLineIgnored(t *testing.T) {
	l := newLog(t)
	l.at(t, "2026-10-02T09:00:00", book("a", 0), book("b", 5*min))
	f, _ := os.OpenFile(string(l), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"ts": "2026-10-0`)
	f.Close()
	eq(t, len(history.Load(string(l))), 1)
}

func TestPythonWrittenRowsLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(`{"ts": "2026-11-08T12:41:45", "key": "A/B.sdr", "title": "B", "total_ms": 22445121, "percent": 0.6467368, "words": 135749, "gained_ms": 104730, "alloc": {"2026-10-30": 20343, "2026-11-08": 84387}}
{"ts": "2026-11-08T12:41:46", "key": "A/C.sdr", "title": "C", "total_ms": 5, "percent": null, "words": null, "gained_ms": 5, "alloc": {"undated": 5}}
`), 0o644)
	rows := history.Load(p)
	eq(t, len(rows), 2)
	eq(t, rows[0].Alloc["2026-11-08"], int64(84387))
	eq(t, *rows[0].Percent, 0.6467368)
	eq(t, rows[1].Percent == nil && rows[1].Words == nil, true)
}

// --- with wake/sleep times (trimmed from Dan's real CoverSleep log, 7-8 Nov 2026) ---

const unb = "Dokic, Jelena/Unbreakable - Jelena Dokic.sdr"
const unbPath = "/mnt/us/documents/Dokic, Jelena/Unbreakable - Jelena Dokic.azw3"

var coversleep = []string{
	"2026-11-06 20:00:00 === Custom screensaver starting ===",
	"2026-11-07 22:38:11 EVENT: outOfScreenSaver 1",
	"2026-11-07 23:10:15 EVENT: goingToScreenSaver 2",
	"2026-11-07 23:10:15 Cover: cache hit for " + unbPath,
	"2026-11-08 10:39:50 EVENT: outOfScreenSaver 1",
	"2026-11-08 10:42:09 EVENT: goingToScreenSaver 2",
	"2026-11-08 10:42:09 Cover: cache hit for " + unbPath,
	"2026-11-08 11:14:49 EVENT: outOfScreenSaver 1",
	"2026-11-08 11:23:19 EVENT: goingToScreenSaver 2",
	"2026-11-08 11:23:19 Cover: cache hit for /mnt/us/documents/Holiday, Ryan/Discipline.azw3",
	"2026-11-08 11:34:44 EVENT: outOfScreenSaver 1",
	"2026-11-08 11:56:12 EVENT: goingToScreenSaver 2",
}

func TestGainIsSplitByAwakeTimeNotPageJumps(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	act := activity.FromSources(nil, activity.ParseCoverSleep(coversleep))
	b := book(unb, 100*min)
	history.Record(p, []books.Book{b}, dt("2026-11-06T20:26:06"), nil)
	b = book(unb, 156*min, "2026-11-07T22:46:46", "2026-11-07T22:49:31")
	rows, _ := history.Record(p, []books.Book{b}, dt("2026-11-08T11:55:55"), act)
	alloc := rows[len(rows)-1].Alloc
	var sum int64
	for _, v := range alloc {
		sum += v
	}
	eq(t, sum, int64(56*min))
	if alloc["2026-11-08"] <= 0 || alloc["2026-11-07"] <= alloc["2026-11-08"] {
		t.Fatalf("alloc %v", alloc)
	}
}

func TestWithoutActivityOldBehaviourStays(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	history.Record(p, []books.Book{book(unb, min)}, dt("2026-11-06T20:00:00"), nil)
	rows, _ := history.Record(p, []books.Book{book(unb, 5*min)}, dt("2026-11-08T11:00:00"),
		activity.FromSources(nil))
	eq(t, rows[len(rows)-1].Alloc, map[string]int64{"2026-11-08": 4 * min})
}
