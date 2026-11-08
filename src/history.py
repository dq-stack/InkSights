"""Append-only reading log built from timer.model snapshots.

The Kindle only keeps a cumulative total per book (no dates), and that total
is lost when a book is re-sent or the device is wiped. So each launch we
compare current totals with the last ones we logged and write one row per
book that gained time:

    {"ts": "2026-10-02T09:15:00", "key": "<sdr path>", "title": "...",
     "total_ms": 8004000, "percent": 0.667, "words": 64392,
     "gained_ms": 120000, "alloc": {"2026-10-01": 120000}}

"alloc" says which days the gained time belongs to. Stats are computed only
from rows' gained/alloc values, never from current totals, so rows can be
merged across backups (dedupe on ts+key) and lost sidecars cost nothing.
"""

import io
import json
import os
from collections import Counter, OrderedDict
from datetime import datetime

UNDATED = "undated"


def load(path):
    rows = []
    if not os.path.exists(path):
        return rows
    with io.open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except ValueError:
                continue  # a torn final line from a power-off must not break stats
    return rows


def last_totals(rows):
    """Latest logged total per sidecar key."""
    totals = {}
    for r in rows:
        totals[r["key"]] = r
    return totals


def _parse(ts):
    try:
        return datetime.strptime(ts[:19], "%Y-%m-%dT%H:%M:%S")
    except (TypeError, ValueError):
        return None


def allocate(gained_ms, history, since, until, baseline, weights=None):
    """Split gained time across days.

    `weights` ({day: awake seconds}, see activity.py) is used first: it says
    when the Kindle was actually in use. Without it, the book's page.history
    timestamps inside (since, until] are used, but those only mark jumps
    (links, Go To), so they're a poor guide. With neither: the first-ever
    (baseline) reading is undated, any later gain goes to the day of `until`.
    """
    days = Counter()
    if weights:
        days.update({d: int(round(w)) for d, w in weights.items() if int(round(w)) > 0})
    for ts in (history or ()) if not days else ():
        t = _parse(ts)
        if t is None or t > until or (since is not None and t <= since):
            continue
        days[t.date().isoformat()] += 1
    if not days:
        key = UNDATED if baseline else until.date().isoformat()
        return OrderedDict([(key, gained_ms)])
    n = sum(days.values())
    alloc = OrderedDict()
    assigned = 0
    ordered = sorted(days)
    for i, d in enumerate(ordered):
        part = gained_ms - assigned if i == len(ordered) - 1 else gained_ms * days[d] // n
        alloc[d] = part
        assigned += part
    return alloc


def build_rows(books, rows, now, activity=None):
    """Rows to append for this launch (books with no timer data are skipped).
    `activity` (activity.Activity) supplies awake time for splitting by day."""
    prev = last_totals(rows)
    first_run = not rows
    last_ts = max((_parse(r["ts"]) for r in rows), default=None)
    out = []
    for b in books:
        total = b.get("total_ms")
        if not total:
            continue
        p = prev.get(b["key"])
        if p is not None and total == p["total_ms"]:
            continue
        if p is None or total < p["total_ms"]:
            # new book, or sidecar was reset (re-send): everything in it is new reading
            gained = total
            since = None if first_run else last_ts
        else:
            gained = total - p["total_ms"]
            since = _parse(p["ts"])
        if gained <= 0:
            continue
        weights = activity.day_weights(since, now, b["key"]) if activity else None
        out.append(OrderedDict([
            ("ts", now.strftime("%Y-%m-%dT%H:%M:%S")),
            ("key", b["key"]),
            ("title", b["title"]),
            ("total_ms", total),
            ("percent", b.get("percent")),
            ("words", b.get("words")),
            ("gained_ms", gained),
            ("alloc", allocate(gained, b.get("history"), since, now, first_run and p is None, weights)),
        ]))
    return out


def record(path, books, now=None, activity=None):
    """Append this launch's rows to the log and return the full log."""
    now = now or datetime.now()
    rows = load(path)
    new = build_rows(books, rows, now, activity)
    if new:
        d = os.path.dirname(path)
        if d and not os.path.isdir(d):
            os.makedirs(d)
        with io.open(path, "a", encoding="utf-8") as f:
            for r in new:
                f.write(json.dumps(r) + "\n")
            f.flush()
            os.fsync(f.fileno())
    return rows + new


def redate(rows, activity):
    """Re-split each row's gained time by awake time, for logs written before
    activity.py existed. Totals and gains are untouched; only "alloc" changes,
    and only for rows a wake/sleep log covers. Returns (rows, changed count)."""
    out, changed = [], 0
    prev = {}
    for r in rows:
        ts = _parse(r.get("ts"))
        p = prev.get(r.get("key"))
        earlier = [t for t in (_parse(x["ts"]) for x in out) if t is not None and ts and t < ts]
        if p is not None and r.get("total_ms", 0) >= p.get("total_ms", 0):
            since = _parse(p["ts"])
        else:
            since = max(earlier) if earlier else None      # new book / reset sidecar
        r = OrderedDict(r)
        weights = activity.day_weights(since, ts, r.get("key")) if ts else None
        if weights:
            alloc = allocate(r["gained_ms"], None, since, ts, False, weights)
            if alloc != r.get("alloc"):
                r["alloc"] = alloc
                changed += 1
        out.append(r)
        prev[r.get("key")] = r
    return out, changed
