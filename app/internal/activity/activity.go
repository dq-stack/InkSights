// Package activity works out when the Kindle was awake, to put reading time
// on the right days.
//
// The Kindle's timer only gives a running total per book, so when InkSights
// sees a book gain 3 hours since its last launch it has to guess which days
// that reading happened on. The book's page.history.store isn't a reading
// log (it only records jumps such as following a link), so it can't answer
// that. Wake/sleep times can: the screen is awake almost only while someone
// is reading, and it sleeps a few minutes after the Kindle is put down.
// Gained time is split across days in proportion to awake time on each day.
//
// Two sources, both plain text logs written on the Kindle:
//
//   - InkSights' own recorder (activity.sh), one line per event:
//     2026-11-08T10:39:50 wake
//     2026-11-08T10:42:10 sleep book /mnt/us/documents/A/B.azw3
//     2026-11-08T11:56:12 sleep library
//     2026-11-08T12:00:00 start
//   - CoverSleep's log (custom_ss.log), which logs every wake and sleep too.
//     Used for days before the recorder existed.
//
// Each source also says when it was running ("coverage"); for any stretch of
// time the source that watched more of it is used. Time in apps that hold
// the screen on without reading (Desk Buddy, from /mnt/us/desk/desk.log) is
// left out.
package activity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dq-stack/InkSights/app/internal/clock"
)

var docRoots = []string{"/mnt/us/documents/", "/mnt/base-us/documents/"}
var bookExts = map[string]bool{".azw3": true, ".azw": true, ".mobi": true, ".kfx": true, ".pdf": true, ".txt": true}

var (
	csLine   = regexp.MustCompile(`^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) (.*)`)
	deskLine = regexp.MustCompile(`^== (start|restored) \w{3} (\w{3}) +(\d+) (\d\d:\d\d:\d\d) .* (\d{4})$`)
	csBook   = regexp.MustCompile(`^Cover: (?:cache hit for|extracted from|none in|book file missing:) (.+)$`)
)

// BookKey turns a book file path on the Kindle into its reading-log key
// ("Author/Title.sdr"), or "" if it isn't a book in documents/.
func BookKey(path string) string {
	for _, root := range docRoots {
		if strings.HasPrefix(path, root) {
			rel := path[len(root):]
			ext := filepath.Ext(rel)
			if bookExts[strings.ToLower(ext)] {
				return strings.TrimSuffix(rel, ext) + ".sdr"
			}
		}
	}
	return ""
}

// Period is a stretch of time the Kindle was awake; Book is the book it went
// to sleep in, if known.
type Period struct {
	Start, End time.Time
	Book       string
}

// Span is a stretch of time.
type Span struct{ Start, End time.Time }

// Source is one log's awake periods and the time it was being written.
type Source struct {
	Name     string
	Periods  []Period
	Coverage []Span
}

func overlap(a, b, lo, hi time.Time) float64 {
	if a.After(lo) {
		lo = a
	}
	if b.Before(hi) {
		hi = b
	}
	if hi.After(lo) {
		return hi.Sub(lo).Seconds()
	}
	return 0
}

// Covered is how many seconds of (since, until] this log watched.
func (s *Source) Covered(since, until time.Time) float64 {
	t := 0.0
	for _, c := range s.Coverage {
		t += overlap(c.Start, c.End, since, until)
	}
	return t
}

type event struct {
	t    time.Time
	kind string
	book string
}

// build turns events (start/stop/wake/sleep, in log order) into a Source.
// A start means the recorder was just launched from the Library, so the
// Kindle is awake at that moment.
func build(name string, events []event) *Source {
	s := &Source{Name: name}
	var seg, awake, last *time.Time
	for _, e := range events {
		t := e.t
		if last != nil && t.Before(*last) {
			continue // clock went backwards (clock set): skip
		}
		switch e.kind {
		case "start":
			if seg != nil { // previous run died without a stop
				if awake != nil && last.After(*awake) {
					s.Periods = append(s.Periods, Period{*awake, *last, ""})
				}
				s.Coverage = append(s.Coverage, Span{*seg, *last})
			}
			seg, awake = &t, &t
		default:
			if seg == nil { // log starts mid-run
				seg = &t
			}
			switch e.kind {
			case "stop":
				if awake != nil {
					s.Periods = append(s.Periods, Period{*awake, t, ""})
				}
				s.Coverage = append(s.Coverage, Span{*seg, t})
				seg, awake = nil, nil
			case "wake":
				awake = &t // a wake while awake means a lost sleep: start over
			case "sleep":
				if awake != nil {
					s.Periods = append(s.Periods, Period{*awake, t, e.book})
				}
				awake = nil
			}
		}
		last = &t
	}
	if seg != nil {
		if awake != nil {
			s.Periods = append(s.Periods, Period{*awake, clock.Open, ""})
		}
		s.Coverage = append(s.Coverage, Span{*seg, clock.Open})
	}
	return s
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

// ParseInkSights reads the recorder's activity.log lines.
func ParseInkSights(lines []string) *Source {
	var events []event
	for _, line := range lines {
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 2 {
			continue
		}
		t, err := time.Parse(clock.Layout, parts[0])
		if err != nil {
			continue
		}
		kind := parts[1]
		if kind != "start" && kind != "stop" && kind != "wake" && kind != "sleep" {
			continue
		}
		book := ""
		if kind == "sleep" && len(parts) == 4 && parts[2] == "book" {
			book = BookKey(parts[3])
		}
		events = append(events, event{t, kind, book})
	}
	return build("inksights", events)
}

// ParseCoverSleep reads CoverSleep's custom_ss.log lines.
func ParseCoverSleep(lines []string) *Source {
	var events []event
	for _, line := range lines {
		m := csLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		t, err := time.Parse("2006-01-02 15:04:05", m[1])
		if err != nil {
			continue
		}
		msg := m[2]
		switch {
		case strings.HasPrefix(msg, "=== Custom screensaver starting"):
			events = append(events, event{t, "start", ""})
		case strings.HasPrefix(msg, "Cleanup complete"):
			events = append(events, event{t, "stop", ""})
		case strings.HasPrefix(msg, "EVENT: outOfScreenSaver"):
			events = append(events, event{t, "wake", ""})
		case strings.HasPrefix(msg, "EVENT: goingToScreenSaver"):
			events = append(events, event{t, "sleep", ""})
		default:
			if b := csBook.FindStringSubmatch(msg); b != nil && len(events) > 0 {
				last := &events[len(events)-1]
				if last.kind == "sleep" && last.book == "" {
					last.book = BookKey(b[1])
				}
			}
		}
	}
	return build("coversleep", events)
}

// ParseDesk reads Desk Buddy sessions (screen kept on, no reading).
func ParseDesk(lines []string) []Span {
	var out []Span
	var start *time.Time
	for _, line := range lines {
		m := deskLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		t, err := time.Parse("Jan 2 2006 15:04:05", m[2]+" "+m[3]+" "+m[5]+" "+m[4])
		if err != nil {
			continue
		}
		if m[1] == "start" {
			if start == nil {
				start = &t
			}
		} else if start != nil {
			out = append(out, Span{*start, t})
			start = nil
		}
	}
	if start != nil {
		out = append(out, Span{*start, clock.Open})
	}
	return out
}

// minus returns the pieces of [a, b) outside every gap.
func minus(a, b time.Time, gaps []Span) []Span {
	pieces := []Span{{a, b}}
	for _, g := range gaps {
		var next []Span
		for _, p := range pieces {
			if !g.End.After(p.Start) || !g.Start.Before(p.End) {
				next = append(next, p)
				continue
			}
			if p.Start.Before(g.Start) {
				next = append(next, Span{p.Start, g.Start})
			}
			if g.End.Before(p.End) {
				next = append(next, Span{g.End, p.End})
			}
		}
		pieces = next
	}
	return pieces
}

// Activity loads the logs only when first asked (most launches never need them).
type Activity struct {
	inkPath, csPath, deskPath string
	sources                   []*Source
	notReading                []Span
	loaded, deskLoaded        bool
}

// New uses these log files (any may be missing).
func New(inksightsLog, coversleepLog, deskLog string) *Activity {
	return &Activity{inkPath: inksightsLog, csPath: coversleepLog, deskPath: deskLog}
}

// FromSources builds an Activity from already-parsed sources (for tests).
func FromSources(notReading []Span, sources ...*Source) *Activity {
	return &Activity{sources: sources, notReading: notReading, loaded: true, deskLoaded: true}
}

func exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func (a *Activity) notReadingSpans() []Span {
	if !a.deskLoaded {
		a.deskLoaded = true
		if exists(a.deskPath) {
			a.notReading = ParseDesk(readLines(a.deskPath))
		}
	}
	return a.notReading
}

func (a *Activity) allSources() []*Source {
	if !a.loaded {
		a.loaded = true
		if exists(a.inkPath) {
			a.sources = append(a.sources, ParseInkSights(readLines(a.inkPath)))
		}
		if exists(a.csPath) {
			a.sources = append(a.sources, ParseCoverSleep(readLines(a.csPath)))
		}
	}
	return a.sources
}

// DayWeights returns {day: awake seconds} for reading of book `key` in
// (since, until], or nil when no log watched that stretch.
func (a *Activity) DayWeights(since *time.Time, until time.Time, key string) map[string]float64 {
	if a == nil || since == nil || !until.After(*since) {
		return nil
	}
	var best *Source
	bestCov := 0.0
	for _, src := range a.allSources() {
		cov := src.Covered(*since, until)
		if cov > 0 && (best == nil || cov > bestCov) {
			best, bestCov = src, cov
		}
	}
	if best == nil {
		return nil
	}
	days := map[string]float64{}
	for _, p := range best.Periods {
		if p.Book != "" && p.Book != key {
			continue // the Kindle was asleep in another book
		}
		lo, hi := p.Start, p.End
		if since.After(lo) {
			lo = *since
		}
		if until.Before(hi) {
			hi = until
		}
		if !hi.After(lo) {
			continue
		}
		for _, piece := range minus(lo, hi, a.notReadingSpans()) {
			lo, hi := piece.Start, piece.End
			for hi.After(lo) {
				midnight := clock.Date(lo).AddDate(0, 0, 1)
				end := hi
				if midnight.Before(end) {
					end = midnight
				}
				days[clock.Day(lo)] += end.Sub(lo).Seconds()
				lo = end
			}
		}
	}
	if len(days) == 0 {
		return nil
	}
	return days
}
