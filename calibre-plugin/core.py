"""The backup itself, free of any GUI code so it can be tested on its own.

Only ever reads from the Kindle.
"""

import os
import shutil
import time

SIDECAR_EXTS = (".azw3f", ".yjf", ".mbs", ".azw3r", ".yjr", ".mbp1")
DATA_SUBDIR = "data/kindle-reading"      # inside each book's folder in the library
KEEP_LOG_COPIES = 30


def sidecar_dir(prefix, lpath):
    """The Kindle keeps a book's reading data next to it: Title.azw3 -> Title.sdr/"""
    return os.path.join(prefix, os.path.splitext(lpath)[0] + ".sdr")


def sidecar_files(sdr):
    try:
        names = os.listdir(sdr)
    except OSError:
        return []
    return sorted(n for n in names if n.lower().endswith(SIDECAR_EXTS))


def _same(path_a, path_b):
    try:
        if os.path.getsize(path_a) != os.path.getsize(path_b):
            return False
        with open(path_a, "rb") as a, open(path_b, "rb") as b:
            return a.read() == b.read()
    except OSError:
        return False


def backup_books(db, prefix, books):
    """db: calibre's new_api Cache; prefix: Kindle mount point; books: list of
    (lpath, library book id or None). Returns a summary dict."""
    summary = {"books": 0, "files": 0, "unchanged": 0, "unmatched": 0, "no_data": 0, "errors": []}
    for lpath, book_id in books:
        sdr = sidecar_dir(prefix, lpath)
        names = sidecar_files(sdr)
        if not names:
            summary["no_data"] += 1
            continue
        if book_id is None:
            summary["unmatched"] += 1
            continue
        existing = {}
        try:
            for ef in db.list_extra_files(book_id):
                existing[ef.relpath] = ef.file_path
        except Exception as e:
            summary["errors"].append("%s: %s" % (lpath, e))
            continue
        to_add = {}
        for name in names:
            relpath = "%s/%s" % (DATA_SUBDIR, name)
            src = os.path.join(sdr, name)
            if relpath in existing and _same(src, existing[relpath]):
                summary["unchanged"] += 1
                continue
            to_add[relpath] = src
        if to_add:
            try:
                db.add_extra_files(book_id, to_add, replace=True)
                summary["files"] += len(to_add)
                summary["books"] += 1
            except Exception as e:
                summary["errors"].append("%s: %s" % (lpath, e))
    return summary


def backup_stats_log(prefix, dest_root, today=None):
    """Dated copy of the InkSights app's data folder (its reading log and its
    own sidecar copies), newest KEEP_LOG_COPIES kept. Returns the copy's path
    or None if the Kindle doesn't have the app."""
    src = os.path.join(prefix, "reading-stats")
    if not os.path.isdir(src):
        return None
    dest = os.path.join(dest_root, today or time.strftime("%Y-%m-%d"))
    if os.path.isdir(dest):
        shutil.rmtree(dest)
    shutil.copytree(src, dest, ignore=shutil.ignore_patterns("._*", "card.cache", "*.tmp"))
    dated = sorted(d for d in os.listdir(dest_root) if len(d) == 10 and d[4] == "-" and d[7] == "-")
    for old in dated[:-KEEP_LOG_COPIES]:
        shutil.rmtree(os.path.join(dest_root, old), ignore_errors=True)
    return dest
