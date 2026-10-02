import os
import sys
import tempfile
import unittest
from datetime import date, datetime

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import history  # noqa: E402
import stats  # noqa: E402

MIN = 60000


def book(key, total_ms, history_ts=(), title=None, percent=0.5, words=1000):
    return {"key": key, "title": title or key, "total_ms": total_ms, "percent": percent,
            "words": words, "history": list(history_ts)}


class HistoryTests(unittest.TestCase):
    def setUp(self):
        self.path = os.path.join(tempfile.mkdtemp(), "snapshots.jsonl")

    def run_at(self, when, books):
        return history.record(self.path, books, datetime.fromisoformat(when))

    def test_first_run_without_history_is_undated(self):
        rows = self.run_at("2026-10-02T09:00:00", [book("a", 30 * MIN)])
        self.assertEqual(rows[0]["alloc"], {"undated": 30 * MIN})

    def test_first_run_spreads_by_page_history(self):
        rows = self.run_at("2026-10-02T09:00:00", [book("a", 90 * MIN, [
            "2026-09-30T20:00:00", "2026-10-01T20:00:00", "2026-10-01T21:00:00"])])
        self.assertEqual(rows[0]["alloc"], {"2026-09-30": 30 * MIN, "2026-10-01": 60 * MIN})

    def test_unchanged_total_writes_nothing(self):
        self.run_at("2026-10-02T09:00:00", [book("a", 30 * MIN)])
        rows = self.run_at("2026-10-03T09:00:00", [book("a", 30 * MIN)])
        self.assertEqual(len(rows), 1)

    def test_normal_delta_goes_to_launch_day(self):
        self.run_at("2026-10-02T09:00:00", [book("a", 30 * MIN)])
        rows = self.run_at("2026-10-03T09:00:00", [book("a", 50 * MIN)])
        self.assertEqual(rows[-1]["gained_ms"], 20 * MIN)
        self.assertEqual(rows[-1]["alloc"], {"2026-10-03": 20 * MIN})

    def test_multi_day_gap_uses_history_inside_window_only(self):
        self.run_at("2026-10-02T09:00:00", [book("a", 30 * MIN, ["2026-10-01T20:00:00"])])
        rows = self.run_at("2026-10-06T09:00:00", [book("a", 70 * MIN, [
            "2026-10-01T20:00:00", "2026-10-03T20:00:00", "2026-10-05T20:00:00"])])
        self.assertEqual(rows[-1]["alloc"], {"2026-10-03": 20 * MIN, "2026-10-05": 20 * MIN})

    def test_reset_sidecar_counts_new_total_without_losing_old(self):
        self.run_at("2026-10-02T09:00:00", [book("a", 60 * MIN)])
        rows = self.run_at("2026-10-04T09:00:00", [book("a", 10 * MIN)])
        self.assertEqual(rows[-1]["gained_ms"], 10 * MIN)
        self.assertEqual(stats.compute(rows, date(2026, 10, 4))["lifetime_ms"], 70 * MIN)

    def test_new_book_after_install_is_dated(self):
        self.run_at("2026-10-02T09:00:00", [book("a", 60 * MIN)])
        rows = self.run_at("2026-10-04T09:00:00", [book("a", 60 * MIN), book("b", 15 * MIN)])
        self.assertEqual(rows[-1]["alloc"], {"2026-10-04": 15 * MIN})

    def test_books_without_timer_skipped_and_torn_line_ignored(self):
        self.run_at("2026-10-02T09:00:00", [book("a", None), book("b", 5 * MIN)])
        with open(self.path, "a") as f:
            f.write('{"ts": "2026-10-0')
        self.assertEqual(len(history.load(self.path)), 1)


class StatsTests(unittest.TestCase):
    def rows(self, per_day):
        return [{"ts": d + "T23:00:00", "key": "k", "title": "Book", "total_ms": 0,
                 "percent": 0.99, "words": 0, "gained_ms": ms, "alloc": {d: ms}}
                for d, ms in per_day.items()]

    def test_streaks(self):
        r = self.rows({"2026-09-01": 10 * MIN, "2026-09-02": 10 * MIN, "2026-09-03": 10 * MIN,
                       "2026-10-01": 10 * MIN, "2026-10-02": 2 * MIN, "2026-10-03": 6 * MIN})
        s = stats.compute(r, date(2026, 10, 3))
        self.assertEqual((s["streak"], s["best_streak"]), (1, 3))

    def test_streak_survives_until_end_of_today(self):
        r = self.rows({"2026-10-01": 10 * MIN, "2026-10-02": 10 * MIN})
        self.assertEqual(stats.compute(r, date(2026, 10, 3))["streak"], 2)
        self.assertEqual(stats.compute(r, date(2026, 10, 4))["streak"], 0)

    def test_heatmap_shape_and_levels(self):
        r = self.rows({"2026-10-01": 90 * MIN, "2026-09-30": 10 * MIN})
        grid = stats.compute(r, date(2026, 10, 2))["heatmap"]   # Friday
        self.assertEqual((len(grid), len(grid[0])), (26, 7))
        # this week, Mon..Sun: Wed 30 Sep = 10 min, Thu 1 Oct = 90 min, Fri = today, weekend = future
        self.assertEqual(grid[-1], [0, 0, 1, 4, 0, None, None])

    def test_monthly_year_and_finished(self):
        r = self.rows({"2026-09-01": 60 * MIN, "2026-10-01": 30 * MIN, "2025-12-31": 5 * MIN})
        s = stats.compute(r, date(2026, 10, 2))
        self.assertEqual(s["monthly_ms"][8], 60 * MIN)
        self.assertEqual(s["year_ms"], 90 * MIN)
        self.assertEqual(s["lifetime_ms"], 95 * MIN)
        self.assertEqual(s["finished"], 1)


if __name__ == "__main__":
    unittest.main()
