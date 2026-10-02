"""A cheap fingerprint of everything the card is computed from.

If two launches have the same fingerprint, their cards are byte-for-byte
the same, so a saved card may be shown without recomputing. Anything that
can change a number on the card is in here:

  * today's date (Today, streak, heatmap, month chart)
  * every sidecar file's name, size and modification time (reading time)
  * each book's title and progress from the library database (cc.db)
  * the stats log (snapshots.jsonl) size and modification time
  * the app's own code that turns data into the card

Only stats the files; never decodes them, so it stays fast.
"""

import hashlib
import os
import sqlite3
from datetime import date

# Not books: the clippings text file and the jailbreak marker document
IGNORE_TITLES = {"My Clippings", "JAILBROKEN"}
SIDECAR_EXTS = (".azw3f", ".yjf", ".mbs", ".azw3r", ".yjr", ".mbp1")
DOCUMENTS_PREFIX = "/mnt/us/documents/"

HERE = os.path.dirname(os.path.abspath(__file__))
CODE_FILES = ("card.py", "stats.py", "history.py", "collect.py", "krds.py", "fingerprint.py")


def is_book_sdr(name):
    if not name.endswith(".sdr"):
        return False
    base = name[: -len(".sdr")]
    return base not in IGNORE_TITLES and not base.endswith(".sh")


def _stat_line(path, rel):
    st = os.stat(path)
    return "%s|%d|%d\n" % (rel, st.st_size, st.st_mtime_ns)


def library_rows(ccdb_path):
    """(location, title, percent) for documents, as the card would read them."""
    con = sqlite3.connect("file:%s?mode=ro" % ccdb_path, uri=True)
    try:
        return con.execute(
            "SELECT p_location, p_titles_0_nominal, p_percentFinished FROM Entries "
            "WHERE p_type = 'Entry:Item' AND p_location LIKE ? ORDER BY p_location",
            (DOCUMENTS_PREFIX + "%",)).fetchall()
    finally:
        con.close()


def quick(documents, snapshots_path, today=None):
    """Everything except the library database: date, sidecar file stats, the
    stats log and the app's code. Only stats files, so it's the fast part;
    after reading it nearly always differs, letting us skip the database."""
    h = hashlib.sha1()
    h.update(((today or date.today()).isoformat() + "\n").encode())
    for root, dirs, _files in os.walk(documents):
        dirs.sort()
        for d in dirs:
            if not is_book_sdr(d):
                continue
            sdr = os.path.join(root, d)
            for name in sorted(os.listdir(sdr)):
                if name.lower().endswith(SIDECAR_EXTS):
                    h.update(_stat_line(os.path.join(sdr, name),
                                        os.path.relpath(os.path.join(sdr, name), documents)).encode())
    for path, rel in ((snapshots_path, "snapshots"),) + tuple(
            (os.path.join(HERE, f), f) for f in CODE_FILES):
        try:
            h.update(_stat_line(path, rel).encode())
        except OSError:
            h.update(("missing " + rel).encode())
    return h.digest()


def full(quick_digest, ccdb_path):
    """quick() plus each book's title and progress from the library database."""
    h = hashlib.sha1(quick_digest)
    try:
        for row in library_rows(ccdb_path):
            h.update(repr(row).encode("utf-8"))
    except sqlite3.Error as e:
        h.update(("ccdb-error %s" % e).encode())
    return h.digest()


def compute(documents, ccdb_path, snapshots_path, today=None):
    """(quick, full) fingerprints."""
    q = quick(documents, snapshots_path, today)
    return q, full(q, ccdb_path)
