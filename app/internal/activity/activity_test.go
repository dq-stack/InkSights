package activity

import (
	"reflect"
	"testing"
	"time"

	"github.com/dq-stack/InkSights/app/internal/clock"
)

const unb = "Dokic, Jelena/Unbreakable - Jelena Dokic.sdr"
const unbPath = "/mnt/us/documents/Dokic, Jelena/Unbreakable - Jelena Dokic.azw3"
const otherPath = "/mnt/us/documents/Holiday, Ryan/Discipline.azw3"

func dt(s string) time.Time {
	t, ok := clock.Parse(s)
	if !ok {
		panic(s)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

// Trimmed from Dan's real CoverSleep log, 7-8 Nov 2026.
var coversleep = []string{
	"2026-11-06 20:00:00 === Custom screensaver starting ===",
	"2026-11-06 20:00:01 READY",
	"2026-11-07 22:38:11 EVENT: outOfScreenSaver 1",
	"2026-11-07 22:38:11 Shield stopped",
	"2026-11-07 23:10:15 EVENT: goingToScreenSaver 2",
	"2026-11-07 23:10:15 Cover: cache hit for " + unbPath,
	"2026-11-07 23:10:15 Drawing cover: /tmp/custom_ss_cover_2362351541",
	"2026-11-08 10:39:50 EVENT: outOfScreenSaver 1",
	"2026-11-08 10:42:09 EVENT: goingToScreenSaver 2",
	"2026-11-08 10:42:09 Cover: cache hit for " + unbPath,
	"2026-11-08 11:14:49 EVENT: outOfScreenSaver 1",
	"2026-11-08 11:23:19 EVENT: goingToScreenSaver 2",
	"2026-11-08 11:23:19 Cover: cache hit for " + otherPath,
	"2026-11-08 11:34:44 EVENT: outOfScreenSaver 1",
	"2026-11-08 11:56:12 EVENT: goingToScreenSaver 2",
	"2026-11-08 11:56:12 Drawing (menu): /mnt/us/screensavers/Harbour Lookout.png",
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\ngot  %v\nwant %v", got, want)
	}
}

func TestBookKey(t *testing.T) {
	eq(t, BookKey(unbPath), unb)
	eq(t, BookKey("/mnt/base-us/documents/x.mobi"), "x.sdr")
	eq(t, BookKey("/mnt/us/screensavers/a.png"), "")
}

func TestCoverSleepPeriodsAndBooks(t *testing.T) {
	src := ParseCoverSleep(coversleep)
	eq(t, src.Periods, []Period{
		{dt("2026-11-07T22:38:11"), dt("2026-11-07T23:10:15"), unb},
		{dt("2026-11-08T10:39:50"), dt("2026-11-08T10:42:09"), unb},
		{dt("2026-11-08T11:14:49"), dt("2026-11-08T11:23:19"), "Holiday, Ryan/Discipline.sdr"},
		{dt("2026-11-08T11:34:44"), dt("2026-11-08T11:56:12"), ""},
	})
	eq(t, src.Coverage, []Span{{dt("2026-11-06T20:00:00"), clock.Open}})
}

func TestInkSightsLog(t *testing.T) {
	src := ParseInkSights([]string{
		"2026-11-08T09:00:00 start",
		"2026-11-08T09:01:00 sleep library",
		"2026-11-08T10:00:00 wake",
		"2026-11-08T10:30:00 sleep book " + unbPath,
		"2026-11-08T12:00:00 wake",
		"junk line",
		"2026-11-08T12:10:00 sleep reader",
	})
	eq(t, src.Periods, []Period{
		{dt("2026-11-08T09:00:00"), dt("2026-11-08T09:01:00"), ""},
		{dt("2026-11-08T10:00:00"), dt("2026-11-08T10:30:00"), unb},
		{dt("2026-11-08T12:00:00"), dt("2026-11-08T12:10:00"), ""},
	})
}

func TestRunThatDiedEndsAtItsLastEvent(t *testing.T) {
	src := ParseInkSights([]string{
		"2026-11-08T09:00:00 start",
		"2026-11-08T09:30:00 sleep library",
		"2026-11-08T20:00:00 wake",
		"2026-11-08T20:10:00 sleep library",
		"2026-11-08T21:00:00 wake",
		"2026-11-08T21:20:00 wake", // repeat wake: the sleep was lost, start over
		"2026-11-08T21:25:00 sleep library",
		"2026-11-08T22:00:00 wake",
		"2026-11-09T08:00:00 start", // restart: last run ended at its last event
	})
	eq(t, src.Coverage[0], Span{dt("2026-11-08T09:00:00"), dt("2026-11-08T22:00:00")})
	eq(t, src.Periods[:len(src.Periods)-1], []Period{
		{dt("2026-11-08T09:00:00"), dt("2026-11-08T09:30:00"), ""},
		{dt("2026-11-08T20:00:00"), dt("2026-11-08T20:10:00"), ""},
		{dt("2026-11-08T21:20:00"), dt("2026-11-08T21:25:00"), ""},
	})
	eq(t, src.Periods[len(src.Periods)-1].Start, dt("2026-11-09T08:00:00")) // still going
}

func TestRealCaseSplitsAcross7And8Nov(t *testing.T) {
	w := FromSources(nil, ParseCoverSleep(coversleep)).DayWeights(
		ptr(dt("2026-11-06T20:26:06")), dt("2026-11-08T11:55:55"), unb)
	// 7 Nov 32m04s; 8 Nov 2m19s + 21m11s (the Discipline period is excluded,
	// the final menu-sleep period counts up to the launch)
	eq(t, w, map[string]float64{"2026-11-07": 32*60 + 4, "2026-11-08": 2*60 + 19 + 21*60 + 11})
}

func TestPeriodAcrossMidnightIsSplit(t *testing.T) {
	src := ParseInkSights([]string{"2026-11-07T23:30:00 start", "2026-11-08T00:30:00 sleep book " + unbPath})
	w := FromSources(nil, src).DayWeights(ptr(dt("2026-11-07T00:00:00")), dt("2026-11-08T12:00:00"), unb)
	eq(t, w, map[string]float64{"2026-11-07": 1800, "2026-11-08": 1800})
}

func TestUnwatchedStretchGivesNil(t *testing.T) {
	a := FromSources(nil, ParseInkSights([]string{"2026-11-08T09:00:00 start", "2026-11-08T09:10:00 stop"}))
	if a.DayWeights(ptr(dt("2026-11-01T00:00:00")), dt("2026-11-02T00:00:00"), unb) != nil {
		t.Fatal("expected nil")
	}
	if a.DayWeights(nil, dt("2026-11-02T00:00:00"), unb) != nil {
		t.Fatal("expected nil")
	}
}

func TestSourceThatWatchedMoreWins(t *testing.T) {
	ink := ParseInkSights([]string{"2026-11-08T11:00:00 start", "2026-11-08T11:10:00 sleep library"})
	w := FromSources(nil, ink, ParseCoverSleep(coversleep)).DayWeights(
		ptr(dt("2026-11-07T00:00:00")), dt("2026-11-08T12:00:00"), unb)
	if _, ok := w["2026-11-07"]; !ok {
		t.Fatalf("got %v", w)
	}
}

func TestDeskBuddyTimeIsNotReading(t *testing.T) {
	desk := ParseDesk([]string{
		"== start Sun Nov  1 21:19:28 GMT+11:0100 2026",
		"21:19:30 touch: up to 3 fingers",
		"== app exited (3) Sun Nov  1 21:30:00 GMT+11:0100 2026",
		"== updated, starting the new version",
		"== restored Sun Nov  1 21:49:28 GMT+11:0100 2026",
	})
	eq(t, desk, []Span{{dt("2026-11-01T21:19:28"), dt("2026-11-01T21:49:28")}})
	src := ParseInkSights([]string{"2026-11-01T21:00:00 start", "2026-11-01T22:00:00 sleep library"})
	w := FromSources(desk, src).DayWeights(ptr(dt("2026-11-01T00:00:00")), dt("2026-11-02T00:00:00"), unb)
	eq(t, w, map[string]float64{"2026-11-01": 30 * 60})
}

func TestMissingFilesAreFine(t *testing.T) {
	a := New("/nonexistent/a.log", "/nonexistent/b.log", "")
	if a.DayWeights(ptr(dt("2026-11-07T00:00:00")), dt("2026-11-08T00:00:00"), unb) != nil {
		t.Fatal("expected nil")
	}
}
