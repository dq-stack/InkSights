"""When the Kindle was awake, used to put reading time on the right days.

The Kindle's timer only gives a running total per book, so when InkSights
sees a book gain 3 hours since its last launch it has to guess which days
that reading happened on. The book's page.history.store isn't a reading log
(it only records jumps such as following a link), so it can't answer that.

Wake/sleep times can: on a Kindle the screen is awake almost only while
someone is reading, and the screen sleeps a few minutes after it's put down.
Gained time is split across days in proportion to awake time on each day.

Two sources, both plain text logs written on the Kindle:

  * InkSights' own recorder (activity.sh, started by main.py), one line per
    event:  2026-11-08T10:39:50 wake
            2026-11-08T10:42:10 sleep book /mnt/us/documents/A/B.azw3
            2026-11-08T11:56:12 sleep library
            2026-11-08T12:00:00 start
  * CoverSleep's log (custom_ss.log), if CoverSleep is installed, which logs
    every wake and sleep too. Used for days before the recorder existed.

Each source also says when it was running ("coverage"), so for any stretch
of time the source that watched more of it is used.

Time spent in apps that hold the screen on without reading (Desk Buddy, from
its own log at /mnt/us/desk/desk.log) is left out.
"""

import io
import os
import re
from collections import Counter
from datetime import datetime, timedelta

OPEN = datetime.max          # a period/coverage still going at the end of a log
DOC_ROOTS = ("/mnt/us/documents/", "/mnt/base-us/documents/")
BOOK_EXTS = (".azw3", ".azw", ".mobi", ".kfx", ".pdf", ".txt")

_CS_LINE = re.compile(r"(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) (.*)")
_DESK_LINE = re.compile(r"== (start|restored) \w{3} (\w{3}) +(\d+) (\d\d:\d\d:\d\d) .* (\d{4})$")
_CS_BOOK = re.compile(r"Cover: (?:cache hit for|extracted from|none in|book file missing:) (.+)$")


def book_key(path):
    """Book file path on the Kindle -> sidecar key used in the reading log
    ("Author/Title.sdr"), or None if it isn't a book in documents/."""
    for root in DOC_ROOTS:
        if path.startswith(root):
            rel = path[len(root):]
            base, ext = os.path.splitext(rel)
            if ext.lower() in BOOK_EXTS:
                return base + ".sdr"
    return None


class Source(object):
    """Awake periods [(start, end, book_key or None)] and the stretches of
    time the log was being written ([(start, end)])."""

    def __init__(self, name, periods, coverage):
        self.name = name
        self.periods = periods
        self.coverage = coverage

    def covered(self, since, until):
        return sum(_overlap(a, b, since, until) for a, b in self.coverage)


def _overlap(a, b, lo, hi):
    lo, hi = max(a, lo), min(b, hi)
    return (hi - lo).total_seconds() if hi > lo else 0.0


def _build(name, events):
    """events: [(time, kind, book)] with kind start/stop/wake/sleep, in log
    order. A start means the recorder was just launched from the Library, so
    the Kindle is awake at that moment."""
    periods, coverage = [], []
    seg = awake = last = None
    for t, kind, book in events:
        if last is not None and t < last:
            continue                      # clock went backwards (clock set): skip
        if kind == "start":
            if seg is not None:           # previous run died without a stop
                if awake is not None and last > awake:
                    periods.append((awake, last, None))
                coverage.append((seg, last))
            seg = awake = t
        else:
            if seg is None:               # log starts mid-run
                seg = t
            if kind == "stop":
                if awake is not None:
                    periods.append((awake, t, None))
                coverage.append((seg, t))
                seg = awake = None
            elif kind == "wake":
                awake = t                 # a wake while awake means a lost sleep: start over
            elif kind == "sleep":
                if awake is not None:
                    periods.append((awake, t, book))
                awake = None
        last = t
    if seg is not None:
        if awake is not None:
            periods.append((awake, OPEN, None))
        coverage.append((seg, OPEN))
    return Source(name, periods, coverage)


def _read_lines(path):
    try:
        with io.open(path, "r", encoding="utf-8", errors="replace") as f:
            return f.read().splitlines()
    except (IOError, OSError):
        return []


def parse_inksights(lines):
    events = []
    for line in lines:
        parts = line.split(" ", 3)
        if len(parts) < 2:
            continue
        try:
            t = datetime.strptime(parts[0], "%Y-%m-%dT%H:%M:%S")
        except ValueError:
            continue
        kind = parts[1]
        if kind not in ("start", "stop", "wake", "sleep"):
            continue
        book = None
        if kind == "sleep" and len(parts) == 4 and parts[2] == "book":
            book = book_key(parts[3])
        events.append((t, kind, book))
    return _build("inksights", events)


def parse_coversleep(lines):
    events = []
    for line in lines:
        m = _CS_LINE.match(line)
        if not m:
            continue
        t = datetime.strptime(m.group(1), "%Y-%m-%d %H:%M:%S")
        msg = m.group(2)
        if msg.startswith("=== Custom screensaver starting"):
            events.append((t, "start", None))
        elif msg.startswith("Cleanup complete"):
            events.append((t, "stop", None))
        elif msg.startswith("EVENT: outOfScreenSaver"):
            events.append((t, "wake", None))
        elif msg.startswith("EVENT: goingToScreenSaver"):
            events.append((t, "sleep", None))
        else:
            b = _CS_BOOK.match(msg)
            if b and events and events[-1][1] == "sleep" and events[-1][2] is None:
                events[-1] = (events[-1][0], "sleep", book_key(b.group(1)))
    return _build("coversleep", events)


def parse_desk(lines):
    """[(start, end)] Desk Buddy sessions (screen kept on, no reading)."""
    out, start = [], None
    for line in lines:
        m = _DESK_LINE.match(line.strip())
        if not m:
            continue
        try:
            t = datetime.strptime("%s %s %s %s" % (m.group(2), m.group(3), m.group(5), m.group(4)),
                                  "%b %d %Y %H:%M:%S")
        except ValueError:
            continue
        if m.group(1) == "start":
            start = start or t
        elif start is not None:
            out.append((start, t))
            start = None
    if start is not None:
        out.append((start, OPEN))
    return out


def _minus(a, b, gaps):
    """Pieces of [a, b) outside every (start, end) in gaps."""
    pieces = [(a, b)]
    for g0, g1 in gaps:
        nxt = []
        for p0, p1 in pieces:
            if g1 <= p0 or g0 >= p1:
                nxt.append((p0, p1))
                continue
            if p0 < g0:
                nxt.append((p0, g0))
            if g1 < p1:
                nxt.append((g1, p1))
        pieces = nxt
    return pieces


class Activity(object):
    """Loads the logs only when first asked (most launches never need them)."""

    def __init__(self, inksights_log=None, coversleep_log=None, desk_log=None):
        self._paths = (inksights_log, coversleep_log)
        self._desk_path = desk_log
        self._sources = None
        self._not_reading = None

    @classmethod
    def from_sources(cls, *sources, **kw):
        a = cls()
        a._sources = list(sources)
        a._not_reading = kw.get("not_reading", [])
        return a

    def not_reading(self):
        if self._not_reading is None:
            self._not_reading = []
            if self._desk_path and os.path.exists(self._desk_path):
                self._not_reading = parse_desk(_read_lines(self._desk_path))
        return self._not_reading

    def sources(self):
        if self._sources is None:
            ink, cs = self._paths
            self._sources = []
            if ink and os.path.exists(ink):
                self._sources.append(parse_inksights(_read_lines(ink)))
            if cs and os.path.exists(cs):
                self._sources.append(parse_coversleep(_read_lines(cs)))
        return self._sources

    def day_weights(self, since, until, key):
        """{iso day: awake seconds} for reading of book `key` in (since, until],
        or None when no log watched that stretch."""
        if since is None or until <= since:
            return None
        best = None
        for src in self.sources():
            cov = src.covered(since, until)
            if cov > 0 and (best is None or cov > best[0]):
                best = (cov, src)
        if best is None:
            return None
        days = Counter()
        for a, b, book in best[1].periods:
            if book is not None and book != key:
                continue                  # the Kindle was asleep in another book
            lo, hi = max(a, since), min(b, until)
            if hi <= lo:
                continue
            for lo, hi in _minus(lo, hi, self.not_reading()):
                while hi > lo:
                    midnight = datetime(lo.year, lo.month, lo.day) + timedelta(days=1)
                    end = min(hi, midnight)
                    days[lo.date().isoformat()] += (end - lo).total_seconds()
                    lo = end
        return days or None
