#!/usr/bin/env python3
"""Mac design preview of the stats card, using the same drawing code as the
Kindle (src/card.py) with Helvetica Neue standing in for Amazon Ember.

Usage: python3 tools/mockup.py <documents-sample-dir> <out-dir> [background.png]
Writes card-real.png (your data) and card-demo.png (made-up fuller data).
"""

import os
import random
import sys
import tempfile
from datetime import date, timedelta

from PIL import Image

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))
import card  # noqa: E402
import collect  # noqa: E402
import history  # noqa: E402
import stats  # noqa: E402

SCREEN = (758, 1024)        # PW2 portrait
MARGIN_TOP = 132            # Library menu bar ends at y=116 (titleBar 41 + searchBar 75)
HN = "/System/Library/Fonts/HelveticaNeue.ttc"
font = card.Fonts(HN, HN, ttc_index=True)


def compose(c, background=None):
    if background:
        screen = Image.open(background).convert("L").resize(SCREEN)
    else:
        screen = Image.new("L", SCREEN, 0xF2)
    x = (SCREEN[0] - c.width) // 2
    y = max(MARGIN_TOP, (SCREEN[1] - c.height) // 2)
    w, h = card.shadow_size(c)
    behind = screen.crop((x, y, x + w, y + h))
    screen.paste(card.frame(c, behind), (x, y))
    return screen


def demo_rows(today):
    rnd = random.Random(7)
    titles = ["Never Split the Difference", "Shoe Dog", "Thinking in Systems",
              "Kitchen Confidential", "The Secret Teachings of All Ages", "Wild Fermentation"]
    pct = {t: 0 for t in titles}
    rows = []
    d = today - timedelta(days=180)
    while d <= today:
        if rnd.random() < 0.62:
            t = rnd.choice(titles[:4]) if rnd.random() < 0.8 else rnd.choice(titles)
            ms = rnd.choice([8, 15, 25, 40, 55, 75, 95]) * 60000
            pct[t] = min(1.0, pct[t] + rnd.uniform(0.02, 0.07))
            rows.append({"ts": d.isoformat() + "T22:00:00", "key": t, "title": t, "total_ms": 0,
                         "percent": pct[t], "words": ms // 60000 * 260, "gained_ms": ms,
                         "alloc": {d.isoformat(): ms}})
        d += timedelta(days=1)
    for r in rows:   # make pace work: total_ms = cumulative minutes
        r["total_ms"] = r["gained_ms"]
    return rows


def main(argv):
    sample, out = argv[1], argv[2]
    bg = argv[3] if len(argv) > 3 else None
    today = date.today()
    log = os.path.join(tempfile.mkdtemp(), "snapshots.jsonl")
    rows = history.record(log, collect.collect(sample, os.path.join(sample, "cc.db")))
    os.makedirs(out, exist_ok=True)
    for name, r in (("real", rows), ("demo", demo_rows(today))):
        path = os.path.join(out, "card-%s.png" % name)
        compose(card.draw_card(stats.compute(r, today), today, font), bg).save(path)
        print(path)


if __name__ == "__main__":
    main(sys.argv)
