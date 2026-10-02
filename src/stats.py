"""Turn the reading log (history.py rows) into the numbers the card shows."""

from collections import OrderedDict, defaultdict
from datetime import date, timedelta

from history import UNDATED

STREAK_MIN_MS = 5 * 60 * 1000   # a day counts toward a streak at 5+ minutes
FINISHED_PERCENT = 0.98
HEATMAP_WEEKS = 26


def per_day(rows):
    days = defaultdict(int)
    for r in rows:
        for d, ms in (r.get("alloc") or {}).items():
            days[d] += ms
    return days


def streaks(days, today):
    """(current, best) runs of consecutive days at or above STREAK_MIN_MS.

    The current streak survives until the end of today, so it still counts
    yesterday's run if you haven't read yet today.
    """
    active = sorted(date.fromisoformat(d) for d, ms in days.items()
                    if d != UNDATED and ms >= STREAK_MIN_MS)
    best = run = 0
    prev = None
    for d in active:
        run = run + 1 if prev is not None and d - prev == timedelta(days=1) else 1
        best = max(best, run)
        prev = d
    current = 0
    if active and active[-1] >= today - timedelta(days=1):
        current = 1
        for a, b in zip(reversed(active[:-1]), reversed(active[1:])):
            if b - a != timedelta(days=1):
                break
            current += 1
    return current, best


def heat_level(ms):
    minutes = ms / 60000.0
    if minutes <= 0:
        return 0
    if minutes < 15:
        return 1
    if minutes < 30:
        return 2
    if minutes < 60:
        return 3
    return 4


def heatmap(days, today, weeks=HEATMAP_WEEKS):
    """weeks x 7 grid of levels 0-4, columns oldest→newest, rows Mon→Sun.
    Days after today are None."""
    end = today + timedelta(days=6 - today.weekday())       # Sunday of this week
    start = end - timedelta(days=weeks * 7 - 1)
    grid = []
    for w in range(weeks):
        col = []
        for dow in range(7):
            d = start + timedelta(days=w * 7 + dow)
            col.append(None if d > today else heat_level(days.get(d.isoformat(), 0)))
        grid.append(col)
    return grid


def compute(rows, today=None):
    today = today or date.today()
    days = per_day(rows)
    year = str(today.year)

    books = OrderedDict()
    for r in rows:
        b = books.setdefault(r["title"], {"title": r["title"], "ms": 0, "percent": None,
                                          "words": 0, "timer_ms": 0, "years": set()})
        b["ms"] += r.get("gained_ms", 0)
        if r.get("percent") is not None:
            b["percent"] = r["percent"]
        b["years"].update(d[:4] for d in (r.get("alloc") or {}) if d != UNDATED)
        # latest timer totals per sidecar give pace; keep per title
        b.setdefault("_latest", {})[r["key"]] = (r.get("words") or 0, r.get("total_ms") or 0)

    words = timer_ms = 0
    for b in books.values():
        for w, t in b.pop("_latest").values():
            words += w
            timer_ms += t

    top = sorted(books.values(), key=lambda b: -b["ms"])
    monthly = [0] * 12
    for d, ms in days.items():
        if d.startswith(year + "-"):
            monthly[int(d[5:7]) - 1] += ms

    current, best = streaks(days, today)
    dated = sorted(d for d in days if d != UNDATED)
    return {
        "today_ms": days.get(today.isoformat(), 0),
        "lifetime_ms": sum(days.values()),
        "streak": current,
        "best_streak": best,
        "year_ms": sum(monthly),
        "year_books": sum(1 for b in books.values() if year in b["years"]),
        "books": len(books),
        "finished": sum(1 for b in books.values() if (b["percent"] or 0) >= FINISHED_PERCENT),
        "wpm": int(words / (timer_ms / 60000.0)) if timer_ms else None,
        "monthly_ms": monthly,
        "top": [{"title": b["title"], "ms": b["ms"], "percent": b["percent"]} for b in top],
        "heatmap": heatmap(days, today),
        "since": dated[0] if dated else None,
    }
