#!/usr/bin/env python3
"""Re-split past reading time by day using the Kindle's wake/sleep logs.

For reading logs written before InkSights recorded wake/sleep times. Only the
per-day split changes; totals don't. Dry run by default (prints old vs new
minutes per day); --write saves it, keeping a copy as snapshots.jsonl.bak-<time>.

    tools/redate-history.py [--kindle /Volumes/Kindle] [--write]
"""
import argparse
import io
import json
import os
import shutil
import sys
from collections import Counter
from datetime import datetime

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "src"))
import activity  # noqa: E402
import history  # noqa: E402


def per_day(rows):
    c = Counter()
    for r in rows:
        for d, ms in r.get("alloc", {}).items():
            c[d] += ms
    return c


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--kindle", default="/Volumes/Kindle")
    ap.add_argument("--write", action="store_true")
    a = ap.parse_args()
    snaps = os.path.join(a.kindle, "reading-stats", "snapshots.jsonl")
    act = activity.Activity(os.path.join(a.kindle, "reading-stats", "activity.log"),
                            os.path.join(a.kindle, "extensions", "custom-screensaver", "custom_ss.log"),
                            os.path.join(a.kindle, "desk", "desk.log"))
    rows = history.load(snaps)
    new, changed = history.redate(rows, act)
    old_d, new_d = per_day(rows), per_day(new)
    print("%-10s %8s %8s" % ("day", "before", "after"))
    for d in sorted(set(old_d) | set(new_d)):
        o, n = old_d.get(d, 0) / 60000.0, new_d.get(d, 0) / 60000.0
        if round(o) != round(n):
            print("%-10s %7.0fm %7.0fm" % (d, o, n))
    print("%d of %d rows re-split; total %.1f h before, %.1f h after"
          % (changed, len(rows), sum(old_d.values()) / 3.6e6, sum(new_d.values()) / 3.6e6))
    if a.write and changed:
        bak = snaps + ".bak-" + datetime.now().strftime("%Y%m%d-%H%M%S")
        shutil.copy2(snaps, bak)
        tmp = snaps + ".tmp"
        with io.open(tmp, "w", encoding="utf-8") as f:
            for r in new:
                f.write(json.dumps(r) + "\n")
        os.replace(tmp, snaps)
        print("written; backup at", bak)


if __name__ == "__main__":
    main()
