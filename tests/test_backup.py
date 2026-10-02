import os
import shutil
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import backup  # noqa: E402


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.docs = os.path.join(self.tmp, "documents")
        self.dest = os.path.join(self.tmp, "reading-stats", "sidecars")
        self.sdr = os.path.join(self.docs, "Voss, Chris", "Never Split - Chris Voss.sdr")
        os.makedirs(self.sdr)
        os.makedirs(os.path.join(self.docs, "Custom Screensaver.sh.sdr"))
        for name, data in (("book.azw3f", b"timer"), ("book.azw3r", b"notes"), ("book.apnx", b"pages")):
            with open(os.path.join(self.sdr, name), "wb") as f:
                f.write(data)

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def copy(self, rel):
        return os.path.join(self.dest, "Voss, Chris", "Never Split - Chris Voss.sdr", rel)

    def test_copies_sidecars_only_and_skips_unchanged(self):
        self.assertEqual(backup.mirror_sidecars(self.docs, self.dest), 2)
        self.assertTrue(os.path.exists(self.copy("book.azw3f")))
        self.assertFalse(os.path.exists(self.copy("book.apnx")))       # not reading data
        self.assertEqual(backup.mirror_sidecars(self.docs, self.dest), 0)

    def test_changed_sidecar_is_recopied(self):
        backup.mirror_sidecars(self.docs, self.dest)
        path = os.path.join(self.sdr, "book.azw3f")
        with open(path, "wb") as f:
            f.write(b"timer, read more")
        future = time.time() + 10
        os.utime(path, (future, future))
        self.assertEqual(backup.mirror_sidecars(self.docs, self.dest), 1)
        with open(self.copy("book.azw3f"), "rb") as f:
            self.assertEqual(f.read(), b"timer, read more")

    def test_copy_survives_the_book_being_deleted(self):
        backup.mirror_sidecars(self.docs, self.dest)
        shutil.rmtree(self.sdr)                       # e.g. Calibre re-send
        backup.mirror_sidecars(self.docs, self.dest)
        self.assertTrue(os.path.exists(self.copy("book.azw3r")))

    def test_originals_untouched(self):
        st = os.stat(os.path.join(self.sdr, "book.azw3f"))
        backup.mirror_sidecars(self.docs, self.dest)
        st2 = os.stat(os.path.join(self.sdr, "book.azw3f"))
        self.assertEqual((st.st_size, st.st_mtime_ns), (st2.st_size, st2.st_mtime_ns))


if __name__ == "__main__":
    unittest.main()
