#!/usr/bin/env python3
"""InkSights: show the Reading Insights card over the Kindle Library.

Launched by the "InkSights" scriptlet. Flow:
  1. cover the Library area (below the menu bar) with an invisible X window,
     so the Library can't paint over the card and taps can't open books;
  2. show the card saved last time straight away (no Pillow needed, so it's
     quick);
  3. read reading time from the book sidecars, log it, compute stats, draw a
     fresh card and update the screen only if it changed;
  4. close on the first tap anywhere (menu-bar taps also reach the Kindle,
     so Home/Settings/etc. work), on sleep, or after a 10-minute backstop.

Kindle system files are only ever read.
"""

import time

T_START = time.time()   # before the other imports, to measure launch cost

import fcntl  # noqa: E402
import logging  # noqa: E402
import os  # noqa: E402
import select  # noqa: E402
import signal  # noqa: E402
import struct  # noqa: E402
import subprocess  # noqa: E402
import sys  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
if sys.flags.no_site:
    # launched with -S (skips scanning ~70 add-on packages at start-up);
    # still need site-packages for Pillow and FBInk
    _site = os.path.join(sys.prefix, "lib", "python%d.%d" % sys.version_info[:2], "site-packages")
    if os.path.isdir(_site):
        sys.path.append(_site)

DATA_DIR = "/mnt/us/reading-stats"
DOCUMENTS = "/mnt/us/documents"
LOG = os.path.join(DATA_DIR, "stats.log")
SNAPSHOTS = os.path.join(DATA_DIR, "snapshots.jsonl")
CARD_CACHE = os.path.join(DATA_DIR, "card.cache")
SIDECAR_BACKUP = os.path.join(DATA_DIR, "sidecars")
LOCK = "/tmp/reading-stats.lock"
CCDB = "/var/local/cc.db"

BACKSTOP_SECS = 10 * 60
RECHECK_AFTER = 3.0         # the Library refreshes ~1-2 s after a scriptlet launch
CARD_GAP = 16               # space between the menu bar and the card
DESIGN_WIDTH = 758          # PW2; other screens scale from this
FALLBACK_TOP = 116          # PW2 menu bar height if the Library window isn't found
CACHE_MAGIC = b"RSC5"
CARD_W, SHADOW = 660, 6     # must match card.CARD_W / card.frame's shadow offset

log = logging.getLogger("reading-stats")


def since_start():
    return time.time() - T_START


def setup_logging():
    if not os.path.isdir(DATA_DIR):
        os.makedirs(DATA_DIR)
    if os.path.exists(LOG) and os.path.getsize(LOG) > 256 * 1024:
        os.replace(LOG, LOG + ".1")
    logging.basicConfig(filename=LOG, level=logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")


def single_instance():
    fd = os.open(LOCK, os.O_CREAT | os.O_RDWR)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        return None
    return fd


def library_area(xd, screen_w, screen_h):
    """(top, height) of the Library below the menu bar."""
    geo = None
    try:
        # the application window, not the Library's "⋮" menu dialog, which
        # shares the booklet id ("L:D_N:non-centered_ID:com.lab126.booklet.home…")
        geo = xd.find_window("N:application_ID:com.lab126.booklet.home")
    except Exception as e:
        log.warning("window lookup failed: %s", e)
    if geo and geo[1] > 0:
        return geo[1], geo[3]
    top = int(FALLBACK_TOP * screen_w / DESIGN_WIDTH)
    return top, screen_h - top


# --- card cache: the last framed card as raw grey bytes, plus where the
# Library window was, so the next launch can draw before asking X anything ---

def load_cache():
    """dict(x, y, w, h, top, lib_h, fp, runs, data) from last time, or None."""
    try:
        with open(CARD_CACHE, "rb") as f:
            head = f.read(60)
            if head[:4] != CACHE_MAGIC:
                return None
            x, y, w, h, top, lib_h = struct.unpack("<HHHHHH", head[4:16])
            fp = (head[16:36], head[36:56])          # (quick, full)
            n_runs, = struct.unpack("<I", head[56:60])
            runs = list(struct.iter_unpack("<HHH", f.read(6 * n_runs)))
            data = f.read()
    except (OSError, struct.error):
        return None
    if len(data) != w * h or len(runs) != n_runs:
        return None
    return dict(x=x, y=y, w=w, h=h, top=top, lib_h=lib_h, fp=fp, runs=runs, data=data)


def save_cache(x, y, w, h, top, lib_h, fp, runs, data):
    tmp = CARD_CACHE + ".tmp"
    with open(tmp, "wb") as f:
        f.write(CACHE_MAGIC + struct.pack("<HHHHHH", x, y, w, h, top, lib_h) + fp[0] + fp[1]
                + struct.pack("<I", len(runs)) + b"".join(struct.pack("<HHH", *r) for r in runs)
                + data)
    os.replace(tmp, CARD_CACHE)


def over_background(data, w, runs, background):
    """The saved card with its see-through pixels taken from the screen now."""
    out = bytearray(data)
    for row, start, length in runs:
        i = row * w + start
        out[i:i + length] = background[i:i + length]
    return bytes(out)


def current_fingerprint():
    import fingerprint
    return fingerprint.compute(DOCUMENTS, CCDB, SNAPSHOTS)


def matches_saved(saved_fp):
    """True only if every input is unchanged. Checks the cheap file part
    first and only queries the library database if that already matches."""
    import fingerprint
    q = fingerprint.quick(DOCUMENTS, SNAPSHOTS)
    if q != saved_fp[0]:
        return False
    return fingerprint.full(q, CCDB) == saved_fp[1]


def fresh_card(region_w, region_h, background, scale):
    """Compute stats and draw the card over `background` (the Library as it
    was before we drew anything). Returns grey bytes for the whole region, so
    a shorter card than last time also clears the old one's leftovers."""
    t = time.time()
    marks = []

    def mark(what):
        marks.append("%s %.2f" % (what, time.time() - t))

    from datetime import date
    from PIL import Image
    mark("PIL")
    import card
    mark("card+fonts")
    import collect
    mark("collect+krds")
    import history
    import stats
    mark("history+stats")
    log.info("imports: %s", ", ".join(marks))

    t = time.time()
    books = collect.collect(DOCUMENTS, CCDB)
    for b in books:
        for e in b["errors"]:
            log.warning("sidecar %s: %s", b["key"], e)
    rows = history.record(SNAPSHOTS, books)
    s = stats.compute(rows, date.today())
    log.info("collected %d books, %d log rows in %.2fs", len(books), len(rows), time.time() - t)

    font = card.Fonts(*card.KINDLE_FONTS, scale=scale)
    c = card.draw_card(s, date.today(), font, scale)
    bg = Image.frombytes("L", (region_w, region_h), background)
    w, h = card.shadow_size(c, scale)
    bg.paste(card.frame(c, bg.crop((0, 0, w, h)), scale), (0, 0))
    return bg, h, card.see_through_runs(c, scale)


def card_region(scr, top, scale):
    """(x, y, w, h) of the area the card (plus shadow) may cover."""
    card_w = int(round(CARD_W * scale))
    region_w = card_w + int(round(SHADOW * scale))
    x = (scr.width - card_w) // 2
    y = top + int(CARD_GAP * scale)
    return x, y, region_w, scr.height - y


def run():
    import screen as screen_mod

    log.info("priority: nice %d", os.nice(0))
    scr = screen_mod.Screen()
    scale = scr.width / float(DESIGN_WIDTH)
    launched = os.environ.get("RS_LAUNCHED")
    after_tap = (lambda: ", %.1fs after tap" % (time.time() - float(launched))) if launched else (lambda: "")

    # 1. Last time's card is shown only if it was made from exactly the data we
    #    have now (same fingerprint): then it IS the correct card, and showing
    #    it needs nothing but FBInk. Otherwise we compute first. Never stale.
    cached = load_cache()
    if cached and cached["w"] != card_region(scr, cached["top"], scale)[2]:
        cached = None                              # different screen/scale: ignore
    top = lib_h = None
    valid = False
    if cached:
        top, lib_h = cached["top"], cached["lib_h"]   # skips the slow window search
        t_fp = time.time()
        valid = matches_saved(cached["fp"])
        log.info("fingerprint %.2fs: %s", time.time() - t_fp,
                 "unchanged, showing saved card" if valid else "data changed, recomputing")

    x = y = region_w = region_h = None
    if valid:
        x, y, region_w, region_h = card_region(scr, top, scale)
        cached["data"] = over_background(cached["data"], cached["w"], cached["runs"],
                                         scr.grab(x, y, cached["w"], cached["h"]))
        scr.show(cached["data"], cached["w"], cached["h"], x, y)
        log.info("card shown %.2fs after python start%s", since_start(), after_tap())

    # 2. The invisible window over the Library (at last time's position if known).
    import touch as touch_mod
    import xshield

    xd = shield = None
    remembered = top is not None
    try:
        xd = xshield.Display(os.environ.get("DISPLAY", ":0"))
        if top is None:
            top, lib_h = library_area(xd, scr.width, scr.height)
        shield = xd.shield(0, top, scr.width, lib_h)
        log.info("shield at y=%d h=%d (%.2fs)", top, lib_h, since_start())
    except Exception as e:
        if top is None:
            top = int(FALLBACK_TOP * scale)
            lib_h = scr.height - top
        log.error("no shield, card may be painted over: %s", e)

    tch = touch_mod.Touch()
    sleep_watch = None
    try:
        sleep_watch = subprocess.Popen(["lipc-wait-event", "com.lab126.powerd", "goingToScreenSaver"],
                                       stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    except OSError as e:
        log.warning("no sleep watcher: %s", e)

    try:
        if valid:
            data, draw_h = cached["data"], cached["h"]
        else:
            # 3. Compute fresh stats and draw them over the Library as it is now.
            x, y, region_w, region_h = card_region(scr, top, scale)
            background = scr.grab(x, y, region_w, region_h)
            img, card_h, runs = fresh_card(region_w, region_h, background, scale)
            draw_h = card_h
            data = img.crop((0, 0, region_w, draw_h)).tobytes()
            scr.show(data, region_w, draw_h, x, y)
            log.info("card shown %.2fs after python start%s", since_start(), after_tap())

            # 4. Remembered Library position still right? (not time-critical now)
            save_top, save_h = top, lib_h
            if remembered and xd is not None:
                real_top, real_h = library_area(xd, scr.width, scr.height)
                if (real_top, real_h) != (top, lib_h):
                    log.warning("Library moved to y=%d h=%d; next launch will use it", real_top, real_h)
                    save_top, save_h = real_top, real_h
            # fingerprint AFTER computing: logging this launch's reading changes the inputs
            save_cache(x, y, region_w, card_h, save_top, save_h, current_fingerprint(), runs, data)

            # 5. Reading data changed, so refresh the sidecar backups (card's already up).
            try:
                import backup
                t_b = time.time()
                n = backup.mirror_sidecars(DOCUMENTS, SIDECAR_BACKUP)
                log.info("backed up %d sidecar file(s) in %.2fs", n, time.time() - t_b)
            except Exception as e:
                log.warning("sidecar backup failed: %s", e)

        drawn = scr.grab(x, y, region_w, draw_h)   # as stored, for the later check

        checked = False
        t_check = time.time() + RECHECK_AFTER
        deadline = T_START + BACKSTOP_SECS
        fds = list(tch.fds) + ([sleep_watch.stdout] if sleep_watch else [])
        while True:
            now = time.time()
            if now >= deadline:
                log.info("closed: backstop timeout")
                break
            wait = deadline - now
            if not checked:
                wait = min(wait, max(0.0, t_check - now))
            ready, _, _ = select.select(fds, [], [], wait)
            if not ready:
                if not checked:
                    # only redraw if something actually painted over the card
                    if scr.grab(x, y, region_w, draw_h) != drawn:
                        scr.show(data, region_w, draw_h, x, y)
                        log.info("card was painted over; redrawn")
                    checked = True
                continue
            if sleep_watch and sleep_watch.stdout in ready:
                log.info("closed: going to sleep")
                break
            tap = None
            for fd in ready:
                if fd in tch.fds:
                    tap = tch.read(fd) or tap
            if tap:
                log.info("closed: tap at %s (%s)", tap,
                         "menu bar" if tap[1] is not None and tap[1] < top else "card/library")
                break
    finally:
        if xd is not None:
            try:
                if shield:
                    xd.unmap(shield)
            except Exception as e:
                log.warning("unmap failed: %s", e)
            xd.close()
        tch.close()
        if sleep_watch and sleep_watch.poll() is None:
            sleep_watch.terminate()
        scr.close()


def main():
    setup_logging()
    if single_instance() is None:
        log.info("already open")
        return 0
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    try:
        run()
    except Exception:
        log.exception("crashed")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
