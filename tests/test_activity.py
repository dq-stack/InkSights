import os
import sys
import tempfile
import unittest
from datetime import datetime

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import activity  # noqa: E402
import history  # noqa: E402

MIN = 60000
UNB = "Dokic, Jelena/Unbreakable - Jelena Dokic.sdr"
UNB_PATH = "/mnt/us/documents/Dokic, Jelena/Unbreakable - Jelena Dokic.azw3"
OTHER_PATH = "/mnt/us/documents/Holiday, Ryan/Discipline.azw3"


def dt(s):
    return datetime.fromisoformat(s)


# Trimmed from Dan's real CoverSleep log, 7-8 Nov 2026.
COVERSLEEP = """\
2026-11-06 20:00:00 === Custom screensaver starting ===
2026-11-06 20:00:01 READY
2026-11-07 22:38:11 EVENT: outOfScreenSaver 1
2026-11-07 22:38:11 Shield stopped
2026-11-07 23:10:15 EVENT: goingToScreenSaver 2
2026-11-07 23:10:15 Cover: cache hit for {unb}
2026-11-07 23:10:15 Drawing cover: /tmp/custom_ss_cover_2362351541
2026-11-08 10:39:50 EVENT: outOfScreenSaver 1
2026-11-08 10:42:09 EVENT: goingToScreenSaver 2
2026-11-08 10:42:09 Cover: cache hit for {unb}
2026-11-08 11:14:49 EVENT: outOfScreenSaver 1
2026-11-08 11:23:19 EVENT: goingToScreenSaver 2
2026-11-08 11:23:19 Cover: cache hit for {other}
2026-11-08 11:34:44 EVENT: outOfScreenSaver 1
2026-11-08 11:56:12 EVENT: goingToScreenSaver 2
2026-11-08 11:56:12 Drawing (menu): /mnt/us/screensavers/Harbour Lookout.png
""".format(unb=UNB_PATH, other=OTHER_PATH)


class ParseTests(unittest.TestCase):
    def test_book_key(self):
        self.assertEqual(activity.book_key(UNB_PATH), UNB)
        self.assertEqual(activity.book_key("/mnt/base-us/documents/x.mobi"), "x.sdr")
        self.assertIsNone(activity.book_key("/mnt/us/screensavers/a.png"))

    def test_coversleep_periods_and_books(self):
        src = activity.parse_coversleep(COVERSLEEP.splitlines())
        self.assertEqual(src.periods, [
            (dt("2026-11-07T22:38:11"), dt("2026-11-07T23:10:15"), UNB),
            (dt("2026-11-08T10:39:50"), dt("2026-11-08T10:42:09"), UNB),
            (dt("2026-11-08T11:14:49"), dt("2026-11-08T11:23:19"), "Holiday, Ryan/Discipline.sdr"),
            (dt("2026-11-08T11:34:44"), dt("2026-11-08T11:56:12"), None),
        ])
        self.assertEqual(src.coverage, [(dt("2026-11-06T20:00:00"), activity.OPEN)])

    def test_inksights_log(self):
        lines = [
            "2026-11-08T09:00:00 start",
            "2026-11-08T09:01:00 sleep library",
            "2026-11-08T10:00:00 wake",
            "2026-11-08T10:30:00 sleep book " + UNB_PATH,
            "2026-11-08T12:00:00 wake",
            "junk line",
            "2026-11-08T12:10:00 sleep reader",
        ]
        src = activity.parse_inksights(lines)
        self.assertEqual(src.periods, [
            (dt("2026-11-08T09:00:00"), dt("2026-11-08T09:01:00"), None),
            (dt("2026-11-08T10:00:00"), dt("2026-11-08T10:30:00"), UNB),
            (dt("2026-11-08T12:00:00"), dt("2026-11-08T12:10:00"), None),
        ])

    def test_run_that_died_ends_at_its_last_event(self):
        src = activity.parse_inksights([
            "2026-11-08T09:00:00 start",
            "2026-11-08T09:30:00 sleep library",
            "2026-11-08T20:00:00 wake",
            "2026-11-08T20:10:00 sleep library",
            "2026-11-08T21:00:00 wake",
            "2026-11-08T21:20:00 wake",          # repeat wake: the sleep was lost, start over
            "2026-11-08T21:25:00 sleep library",
            "2026-11-08T22:00:00 wake",
            "2026-11-09T08:00:00 start",          # restart: last run ended at its last event
        ])
        self.assertEqual(src.coverage[0], (dt("2026-11-08T09:00:00"), dt("2026-11-08T22:00:00")))
        self.assertEqual(src.periods[:-1], [
            (dt("2026-11-08T09:00:00"), dt("2026-11-08T09:30:00"), None),
            (dt("2026-11-08T20:00:00"), dt("2026-11-08T20:10:00"), None),
            (dt("2026-11-08T21:20:00"), dt("2026-11-08T21:25:00"), None),
        ])
        self.assertEqual(src.periods[-1][0], dt("2026-11-09T08:00:00"))   # still going


class WeightTests(unittest.TestCase):
    def test_real_case_splits_across_7_and_8_nov(self):
        act = activity.Activity.from_sources(activity.parse_coversleep(COVERSLEEP.splitlines()))
        w = act.day_weights(dt("2026-11-06T20:26:06"), dt("2026-11-08T11:55:55"), UNB)
        # 7 Nov 32m04s; 8 Nov 2m19s + 21m11s (the Discipline period is excluded,
        # the final menu-sleep period counts up to the launch)
        self.assertEqual(w["2026-11-07"], 32 * 60 + 4)
        self.assertEqual(w["2026-11-08"], 2 * 60 + 19 + 21 * 60 + 11)
        self.assertEqual(set(w), {"2026-11-07", "2026-11-08"})

    def test_period_across_midnight_is_split(self):
        src = activity.parse_inksights([
            "2026-11-07T23:30:00 start",
            "2026-11-08T00:30:00 sleep book " + UNB_PATH,
        ])
        w = activity.Activity.from_sources(src).day_weights(
            dt("2026-11-07T00:00:00"), dt("2026-11-08T12:00:00"), UNB)
        self.assertEqual(dict(w), {"2026-11-07": 1800, "2026-11-08": 1800})

    def test_unwatched_stretch_gives_none(self):
        src = activity.parse_inksights(["2026-11-08T09:00:00 start", "2026-11-08T09:10:00 stop"])
        act = activity.Activity.from_sources(src)
        self.assertIsNone(act.day_weights(dt("2026-11-01T00:00:00"), dt("2026-11-02T00:00:00"), UNB))
        self.assertIsNone(act.day_weights(None, dt("2026-11-02T00:00:00"), UNB))

    def test_source_that_watched_more_wins(self):
        ink = activity.parse_inksights([
            "2026-11-08T11:00:00 start",
            "2026-11-08T11:10:00 sleep library",
        ])
        cs = activity.parse_coversleep(COVERSLEEP.splitlines())
        w = activity.Activity.from_sources(ink, cs).day_weights(
            dt("2026-11-07T00:00:00"), dt("2026-11-08T12:00:00"), UNB)
        self.assertIn("2026-11-07", w)

    def test_desk_buddy_time_is_not_reading(self):
        desk = activity.parse_desk([
            "== start Sun Nov  1 21:19:28 GMT+11:0100 2026",
            "21:19:30 touch: up to 3 fingers",
            "== app exited (3) Sun Nov  1 21:30:00 GMT+11:0100 2026",
            "== updated, starting the new version",
            "== restored Sun Nov  1 21:49:28 GMT+11:0100 2026",
        ])
        self.assertEqual(desk, [(dt("2026-11-01T21:19:28"), dt("2026-11-01T21:49:28"))])
        src = activity.parse_inksights([
            "2026-11-01T21:00:00 start",
            "2026-11-01T22:00:00 sleep library",
        ])
        w = activity.Activity.from_sources(src, not_reading=desk).day_weights(
            dt("2026-11-01T00:00:00"), dt("2026-11-02T00:00:00"), UNB)
        self.assertEqual(dict(w), {"2026-11-01": 30 * 60})

    def test_missing_files_are_fine(self):
        act = activity.Activity("/nonexistent/a.log", "/nonexistent/b.log")
        self.assertIsNone(act.day_weights(dt("2026-11-07T00:00:00"), dt("2026-11-08T00:00:00"), UNB))


class HistoryWithActivityTests(unittest.TestCase):
    def test_gain_is_split_by_awake_time_not_page_jumps(self):
        path = os.path.join(tempfile.mkdtemp(), "snapshots.jsonl")
        act = activity.Activity.from_sources(activity.parse_coversleep(COVERSLEEP.splitlines()))
        b = {"key": UNB, "title": "Unbreakable", "total_ms": 100 * MIN, "percent": 0.5,
             "words": 1, "history": []}
        history.record(path, [b], dt("2026-11-06T20:26:06"))
        b = dict(b, total_ms=b["total_ms"] + 56 * MIN,
                 history=["2026-11-07T22:46:46.362000", "2026-11-07T22:49:31.956000"])
        rows = history.record(path, [b], dt("2026-11-08T11:55:55"), activity=act)
        alloc = rows[-1]["alloc"]
        self.assertEqual(sum(alloc.values()), 56 * MIN)
        self.assertGreater(alloc["2026-11-08"], 0)
        self.assertGreater(alloc["2026-11-07"], alloc["2026-11-08"])

    def test_without_activity_old_behaviour_stays(self):
        path = os.path.join(tempfile.mkdtemp(), "snapshots.jsonl")
        b = {"key": UNB, "title": "U", "total_ms": MIN, "percent": 0.1, "words": 1, "history": []}
        history.record(path, [b], dt("2026-11-06T20:00:00"))
        rows = history.record(path, [dict(b, total_ms=5 * MIN)], dt("2026-11-08T11:00:00"),
                              activity=activity.Activity.from_sources())
        self.assertEqual(rows[-1]["alloc"], {"2026-11-08": 4 * MIN})

    def test_redate_fixes_old_rows_and_keeps_totals(self):
        act = activity.Activity.from_sources(activity.parse_coversleep(COVERSLEEP.splitlines()))
        rows = [
            {"ts": "2026-11-06T20:26:06", "key": UNB, "total_ms": 100 * MIN,
             "gained_ms": 100 * MIN, "alloc": {"2026-11-06": 100 * MIN}},
            {"ts": "2026-11-08T11:55:55", "key": UNB, "total_ms": 156 * MIN,
             "gained_ms": 56 * MIN, "alloc": {"2026-11-07": 56 * MIN}},
        ]
        out, changed = history.redate(rows, act)
        self.assertEqual(changed, 1)
        self.assertEqual(out[0]["alloc"], rows[0]["alloc"])     # before the log: kept
        self.assertEqual(sum(out[1]["alloc"].values()), 56 * MIN)
        self.assertGreater(out[1]["alloc"]["2026-11-08"], 0)


if __name__ == "__main__":
    unittest.main()
