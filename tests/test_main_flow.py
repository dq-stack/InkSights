"""Run main.run() on the Mac with a fake framebuffer, X shield and touchscreen."""
import os
import shutil
import sys
import tempfile
import types
import unittest

SRC = os.path.join(os.path.dirname(__file__), "..", "src")
SAMPLE = os.path.join(os.path.dirname(__file__), "..", "sample")
sys.path.insert(0, SRC)

W, H = 758, 1024
EVENTS = []


class FakeScreen(object):
    instances = []

    def __init__(self):
        self.width, self.height = W, H
        self.fb = bytearray(b"\xcc" * (W * H))   # stand-in for the Library
        self.shows = []
        FakeScreen.instances.append(self)

    def grab(self, x, y, w, h):
        return b"".join(bytes(self.fb[(y + r) * W + x:(y + r) * W + x + w]) for r in range(h))

    def show(self, data, w, h, x, y, flash=False):
        assert len(data) == w * h
        for r in range(h):
            self.fb[(y + r) * W + x:(y + r) * W + x + w] = data[r * w:(r + 1) * w]
        self.shows.append((w, h, x, y))
        EVENTS.append("show")

    def close(self):
        pass


class FakeDisplay(object):
    def __init__(self, *_):
        self.unmapped = False
        EVENTS.append("x-connect")

    def find_window(self, needle):
        assert needle == "N:application_ID:com.lab126.booklet.home"
        EVENTS.append("find-window")
        return (0, 116, W, 908)

    def shield(self, *a):
        return 42

    def unmap(self, wid):
        FakeDisplay.unmapped = True

    def close(self):
        pass


class FakeTouch(object):
    def __init__(self):
        r, w = os.pipe()
        os.write(w, b"x")           # a tap is already waiting: close straight away
        self.fds = [r]
        self.paths = ["fake"]

    def read(self, fd):
        os.read(fd, 1)
        return (400, 600)

    def close(self):
        os.close(self.fds[0])


@unittest.skipUnless(os.path.exists(os.path.join(SAMPLE, "cc.db")), "no local sample")
class MainFlowTests(unittest.TestCase):
    def setUp(self):
        import card
        import main
        self.main, self.card = main, card
        self.tmp = tempfile.mkdtemp()
        self.docs = os.path.join(self.tmp, "documents")
        shutil.copytree(SAMPLE, self.docs)           # a copy we can "read" in
        self.saved = (main.DATA_DIR, main.DOCUMENTS, main.SNAPSHOTS, main.CARD_CACHE, main.LOCK,
                      main.CCDB, card.KINDLE_FONTS)
        main.DATA_DIR = self.tmp
        main.DOCUMENTS = self.docs
        main.SNAPSHOTS = os.path.join(self.tmp, "snapshots.jsonl")
        main.CARD_CACHE = os.path.join(self.tmp, "card.cache")
        self.saved_backup = main.SIDECAR_BACKUP
        main.SIDECAR_BACKUP = os.path.join(self.tmp, "sidecars")
        main.LOCK = os.path.join(self.tmp, "lock")
        main.CCDB = os.path.join(self.docs, "cc.db")
        sup = "/System/Library/Fonts/Supplemental/"
        card.KINDLE_FONTS = (sup + "Georgia.ttf", sup + "Georgia Bold.ttf")
        sys.modules["screen"] = types.SimpleNamespace(Screen=FakeScreen)
        sys.modules["xshield"] = types.SimpleNamespace(Display=FakeDisplay)
        sys.modules["touch"] = types.SimpleNamespace(Touch=FakeTouch)
        FakeScreen.instances = []
        FakeDisplay.unmapped = False
        del EVENTS[:]

    def tearDown(self):
        (self.main.DATA_DIR, self.main.DOCUMENTS, self.main.SNAPSHOTS, self.main.CARD_CACHE,
         self.main.LOCK, self.main.CCDB, self.card.KINDLE_FONTS) = self.saved
        for m in ("screen", "xshield", "touch"):
            sys.modules.pop(m, None)
        shutil.rmtree(self.tmp)

    def sidecar(self):
        for root, _dirs, files in os.walk(self.docs):
            for f in files:
                if f.endswith(".azw3f"):
                    return os.path.join(root, f)

    def test_unchanged_data_shows_saved_card_before_any_x_traffic(self):
        self.main.run()
        first = FakeScreen.instances[-1]
        self.assertEqual(len(first.shows), 1)
        w, h, x, y = first.shows[0]
        self.assertEqual((x, y), ((W - 660) // 2, 116 + 16))
        self.assertTrue(FakeDisplay.unmapped)
        self.assertIn("find-window", EVENTS)          # first launch looks the Library up

        del EVENTS[:]
        self.main.run()
        second = FakeScreen.instances[-1]
        self.assertEqual(second.shows, [(w, h, x, y)])  # just the saved card
        self.assertEqual(EVENTS[:2], ["show", "x-connect"])
        self.assertNotIn("find-window", EVENTS)       # remembered position, no search
        self.assertEqual(bytes(second.fb), bytes(first.fb))

    def test_changed_reading_data_never_shows_saved_card(self):
        self.main.run()
        path = self.sidecar()
        st = os.stat(path)
        os.utime(path, ns=(st.st_atime_ns, st.st_mtime_ns + 5 * 10 ** 9))   # "read a book"
        del EVENTS[:]
        self.main.run()
        shows = FakeScreen.instances[-1].shows
        self.assertEqual(len(shows), 1)               # only the freshly computed card
        self.assertLess(EVENTS.index("x-connect"), EVENTS.index("show"))  # computed, not cached

    def test_new_day_never_shows_saved_card(self):
        import fingerprint
        from datetime import date, timedelta
        self.main.run()
        real = fingerprint.quick
        fingerprint.quick = lambda docs, snaps, today=None: real(docs, snaps, today=date.today() + timedelta(days=1))
        try:
            del EVENTS[:]
            self.main.run()
        finally:
            fingerprint.quick = real
        self.assertLess(EVENTS.index("x-connect"), EVENTS.index("show"))

    def test_changed_progress_in_library_db_never_shows_saved_card(self):
        import sqlite3
        self.main.run()
        con = sqlite3.connect(self.main.CCDB)
        con.execute("UPDATE Entries SET p_percentFinished = p_percentFinished + 1 "
                    "WHERE p_titles_0_nominal LIKE 'Never Split%'")
        con.commit()
        con.close()
        del EVENTS[:]
        self.main.run()
        self.assertLess(EVENTS.index("x-connect"), EVENTS.index("show"))

    def test_saved_card_corners_use_the_library_as_it_is_now(self):
        self.main.run()
        w, h, x, y = FakeScreen.instances[-1].shows[0]
        orig_init = FakeScreen.__init__

        def init_new_library(scr):
            orig_init(scr)
            scr.fb = bytearray(b"\x55" * (W * H))      # covers have changed since
        FakeScreen.__init__ = init_new_library
        try:
            self.main.run()
        finally:
            FakeScreen.__init__ = orig_init
        fb = FakeScreen.instances[-1].fb
        self.assertEqual(fb[y * W + x], 0x55)        # top-left corner: today's Library
        self.assertEqual(fb[(y + 1) * W + x + w - 1], 0x55)   # shadow gap at the top right
        mid = (y + 100) * W + x + 300
        self.assertEqual(fb[mid], 0xFF)               # inside the card: card white


if __name__ == "__main__":
    unittest.main()
