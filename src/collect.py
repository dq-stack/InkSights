#!/usr/bin/env python3
"""Walk a Kindle documents folder and pull reading-time data out of the
.sdr sidecars, using the vendored KRDS decoder.

Read-only: sidecar files are opened "rb" and never written.

Progress (%) and titles come from the Kindle's library database (cc.db),
the same source as the % badge on each cover. timer.model's totalPercent is
NOT progress (it's the share of the book's words the timer has measured,
e.g. 67% for a book the Kindle shows at 30%), so it is never used for that.

Usage: python3 collect.py <documents-dir> [--ccdb PATH] [--json]
"""

import io
import json
import logging
import os
import sqlite3
import sys

from fingerprint import SIDECAR_EXTS, is_book_sdr
from krds import KindleReaderDataStore

DEVICE_DOCUMENTS = "/mnt/us/documents/"
DEVICE_CCDB = "/var/local/cc.db"

log = logging.getLogger("collect")


def find_key(obj, key):
    """Depth-first search for the first value stored under `key`."""
    if isinstance(obj, dict):
        if key in obj:
            return obj[key]
        for v in obj.values():
            found = find_key(v, key)
            if found is not None:
                return found
    elif isinstance(obj, list):
        for v in obj:
            found = find_key(v, key)
            if found is not None:
                return found
    return None


def decode_file(path):
    with io.open(path, "rb") as f:
        data = f.read()
    return KindleReaderDataStore(log, data).deserialize()


def title_from_sdr(sdr_dir):
    """Best-effort display title from a sidecar folder name.

    Handles "Title_ Subtitle - Author.sdr" (Calibre), "Title_ASIN.sdr"
    (Amazon/Send to Kindle) and Calibre's "Title, The" sort order.
    """
    name = os.path.basename(sdr_dir)[: -len(".sdr")]
    if " - " in name:
        name = name.rsplit(" - ", 1)[0]
    for article in ("The", "A", "An"):     # Calibre puts it after the subtitle
        if name.endswith(", " + article):
            name = article + " " + name[: -len(article) - 2]
    return name.split("_", 1)[0].strip()


def _ccdb_rows(path):
    query = ("SELECT p_location, p_titles_0_nominal, p_credits_0_name_collation, "
             "p_percentFinished FROM Entries WHERE p_type = 'Entry:Item' "
             "AND p_location LIKE '%s%%'" % DEVICE_DOCUMENTS)
    try:
        con = sqlite3.connect("file:%s?mode=ro" % path, uri=True)
        try:
            return con.execute(query).fetchall()
        finally:
            con.close()
    except sqlite3.Error:
        # e.g. database locked by the Kindle UI: fall back to the system CLI
        import subprocess
        out = subprocess.check_output(["sqlite3", "-separator", "\x1f",
                                       "file:%s?mode=ro" % path, query])
        return [line.split("\x1f") for line in out.decode("utf-8").splitlines()]


def library(ccdb_path):
    """Map sidecar key (relative .sdr path) -> {title, author, percent}.

    Missing or unreadable cc.db just means no titles/progress, never a crash.
    """
    books = {}
    if not ccdb_path or not os.path.exists(ccdb_path):
        return books
    try:
        rows = _ccdb_rows(ccdb_path)
    except Exception as e:
        log.warning("cc.db unreadable: %s", e)
        return books
    for location, title, author, percent in rows:
        rel = location[len(DEVICE_DOCUMENTS):]
        key = os.path.splitext(rel)[0] + ".sdr"
        try:
            pct = float(percent) / 100.0 if percent not in (None, "") else None
        except ValueError:
            pct = None
        books[key] = {
            "title": clean_title(title) if title else None,
            "author": author or None,
            "percent": pct,
        }
    return books


def clean_title(title):
    """Drop the subtitle: 'Shoe Dog: A Memoir by…' -> 'Shoe Dog'."""
    return title.split(": ", 1)[0].strip()


def collect_book(sdr_dir, documents_dir):
    book = {
        "key": os.path.relpath(sdr_dir, documents_dir),
        "title": title_from_sdr(sdr_dir),
        "total_ms": None,
        "words": None,
        "percent": None,
        "history": [],
        "sidecars": [],
        "errors": [],
    }
    for name in sorted(os.listdir(sdr_dir)):
        if not name.lower().endswith(SIDECAR_EXTS):
            continue
        path = os.path.join(sdr_dir, name)
        book["sidecars"].append(name)
        try:
            decoded = decode_file(path)
        except Exception as e:  # partial/unknown data must never stop the run
            book["errors"].append("%s: %s" % (name, e))
            continue
        timer = find_key(decoded, "timer.model")
        if timer and book["total_ms"] is None:
            book["total_ms"] = timer.get("totalTime")
            book["words"] = timer.get("totalWords")
        history = find_key(decoded, "page.history.store")
        if history:
            book["history"].extend(h.get("time") for h in history if isinstance(h, dict))
    return book


def collect(documents_dir, ccdb_path=None):
    lib = library(ccdb_path)
    books = []
    for root, dirs, _files in os.walk(documents_dir):
        for d in sorted(dirs):
            if is_book_sdr(d):
                book = collect_book(os.path.join(root, d), documents_dir)
                meta = lib.get(book["key"])
                if meta:
                    book["title"] = meta["title"] or book["title"]
                    book["author"] = meta["author"]
                    book["percent"] = meta["percent"]
                books.append(book)
    return books


def fmt_hours(ms):
    return "%6.1f h" % (ms / 3600000.0) if ms else "     – "


def report(books):
    timed = [b for b in books if b["total_ms"]]
    untimed = [b for b in books if not b["total_ms"] and not b["errors"]]
    failed = [b for b in books if b["errors"]]
    for b in sorted(books, key=lambda b: -(b["total_ms"] or 0)):
        wpm = ""
        if b["total_ms"] and b["words"]:
            wpm = "%4d wpm" % (b["words"] / (b["total_ms"] / 60000.0))
        pct = "%5.1f%%" % (b["percent"] * 100) if b["percent"] is not None else "     "
        hist = "%3d hist" % len(b["history"]) if b["history"] else "        "
        flag = "  ERR" if b["errors"] else ""
        print("%s  %s  %8s  %s  %s%s" % (fmt_hours(b["total_ms"]), pct, wpm, hist, b["title"][:50], flag))
    total = sum(b["total_ms"] for b in timed)
    print()
    print("Books: %d   with timer data: %d   no timer data: %d   parse errors: %d"
          % (len(books), len(timed), len(untimed), len(failed)))
    print("Lifetime: %.1f hours" % (total / 3600000.0))
    for b in failed:
        for e in b["errors"]:
            print("  ERR %s -> %s" % (b["key"], e))


def main(argv):
    logging.basicConfig(level=logging.WARNING, format="%(levelname)s: %(message)s")
    if len(argv) < 2:
        print(__doc__)
        return 2
    ccdb = argv[argv.index("--ccdb") + 1] if "--ccdb" in argv else None
    if ccdb is None:
        for candidate in (os.path.join(argv[1], "cc.db"), DEVICE_CCDB):
            if os.path.exists(candidate):
                ccdb = candidate
                break
    books = collect(argv[1], ccdb)
    if "--json" in argv:
        json.dump(books, sys.stdout, indent=2, default=str)
    else:
        report(books)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
