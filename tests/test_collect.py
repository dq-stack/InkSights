import os
import sqlite3
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import collect  # noqa: E402


class LibraryTests(unittest.TestCase):
    def make_ccdb(self, rows):
        path = os.path.join(tempfile.mkdtemp(), "cc.db")
        con = sqlite3.connect(path)
        con.execute("CREATE TABLE Entries (p_type, p_location, p_titles_0_nominal, "
                    "p_credits_0_name_collation, p_percentFinished)")
        con.executemany("INSERT INTO Entries VALUES (?,?,?,?,?)", rows)
        con.commit()
        con.close()
        return path

    def test_progress_and_title_come_from_ccdb(self):
        path = self.make_ccdb([
            ("Entry:Item", "/mnt/us/documents/Voss, Chris/Never Split the Difference_ X - Chris Voss.azw3",
             "Never Split the Difference: Negotiating as if Your Life Depended on It", "Chris Voss", 29.726315),
            ("Entry:Item", "/mnt/us/documents/Book_B00X.azw", "Plain Book", None, None),
            ("Entry:Item:Dictionary", "/mnt/us/documents/dict.azw", "Dict", None, 0),
        ])
        lib = collect.library(path)
        b = lib["Voss, Chris/Never Split the Difference_ X - Chris Voss.sdr"]
        self.assertEqual(b["title"], "Never Split the Difference")
        self.assertAlmostEqual(b["percent"], 0.29726315)
        self.assertIsNone(lib["Book_B00X.sdr"]["percent"])
        self.assertNotIn("dict.sdr", lib)

    def test_marked_as_read_counts_as_finished(self):
        """FW 5.12 "Mark as Read" sets p_readState = 1 and leaves the percent."""
        path = os.path.join(tempfile.mkdtemp(), "cc.db")
        con = sqlite3.connect(path)
        con.execute("CREATE TABLE Entries (p_type, p_location, p_titles_0_nominal, "
                    "p_credits_0_name_collation, p_percentFinished, p_readState)")
        con.executemany("INSERT INTO Entries VALUES (?,?,?,?,?,?)", [
            ("Entry:Item", "/mnt/us/documents/A/African History - X.azw3",
             "An African History of Africa", "X", 73.1, 1),
            ("Entry:Item", "/mnt/us/documents/B/Unread - Y.azw3", "Unread", "Y", 12.0, None),
        ])
        con.commit()
        con.close()
        lib = collect.library(path)
        self.assertEqual(lib["A/African History - X.sdr"]["percent"], 1.0)
        self.assertAlmostEqual(lib["B/Unread - Y.sdr"]["percent"], 0.12)

    def test_marking_as_read_changes_the_fingerprint(self):
        import fingerprint
        path = os.path.join(tempfile.mkdtemp(), "cc.db")
        con = sqlite3.connect(path)
        con.execute("CREATE TABLE Entries (p_type, p_location, p_titles_0_nominal, "
                    "p_percentFinished, p_readState)")
        con.execute("INSERT INTO Entries VALUES ('Entry:Item', '/mnt/us/documents/a.azw3', 'A', 73.1, NULL)")
        con.commit()
        before = fingerprint.library_rows(path)
        con.execute("UPDATE Entries SET p_readState = 1")
        con.commit()
        con.close()
        self.assertNotEqual(before, fingerprint.library_rows(path))

    def test_missing_ccdb_is_not_an_error(self):
        self.assertEqual(collect.library("/nonexistent/cc.db"), {})
        self.assertEqual(collect.library(None), {})


class RealSampleTest(unittest.TestCase):
    """Against Dan's copied sample, if present: the Kindle showed 30% on 2 Oct 2026."""
    SAMPLE = os.path.join(os.path.dirname(__file__), "..", "sample")

    @unittest.skipUnless(os.path.exists(os.path.join(SAMPLE, "cc.db")), "no local sample")
    def test_never_split_progress_matches_kindle_badge(self):
        books = collect.collect(self.SAMPLE, os.path.join(self.SAMPLE, "cc.db"))
        ns = [b for b in books if b["title"] == "Never Split the Difference"][0]
        self.assertAlmostEqual(ns["percent"], 0.297, places=3)


if __name__ == "__main__":
    unittest.main()
