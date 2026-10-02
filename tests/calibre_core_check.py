"""Run with: calibre-debug -e tests/calibre_core_check.py
Exercises the Calibre plugin's core against a throwaway calibre library."""
import os
import shutil
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "calibre-plugin"))
import core  # noqa: E402

from calibre.db.legacy import LibraryDatabase  # noqa: E402
from calibre.ebooks.metadata.book.base import Metadata  # noqa: E402

tmp = tempfile.mkdtemp()
try:
    lib = os.path.join(tmp, "library")
    os.makedirs(lib)
    db = LibraryDatabase(lib).new_api
    book_id = db.create_book_entry(Metadata("Never Split the Difference", ["Chris Voss"]))

    kindle = os.path.join(tmp, "Kindle") + os.sep
    sdr = os.path.join(kindle, "documents", "Voss, Chris", "Never Split - Chris Voss.sdr")
    os.makedirs(sdr)
    for name, data in (("x.azw3f", b"timer"), ("x.azw3r", b"notes"), ("x.apnx", b"pages")):
        with open(os.path.join(sdr, name), "wb") as f:
            f.write(data)
    os.makedirs(os.path.join(kindle, "reading-stats", "sidecars"))
    with open(os.path.join(kindle, "reading-stats", "snapshots.jsonl"), "w") as f:
        f.write("{}\n")
    with open(os.path.join(kindle, "reading-stats", "card.cache"), "w") as f:
        f.write("x")

    books = [("documents/Voss, Chris/Never Split - Chris Voss.azw3", book_id),
             ("documents/Unknown/Not In Library.azw3", None),
             ("documents/Empty/No Data.azw3", 99)]
    os.makedirs(os.path.join(kindle, "documents", "Unknown", "Not In Library.sdr"))
    with open(os.path.join(kindle, "documents", "Unknown", "Not In Library.sdr", "y.mbs"), "wb") as f:
        f.write(b"z")

    s1 = core.backup_books(db, kindle, books)
    print("first:", s1)
    assert (s1["files"], s1["books"], s1["unmatched"], s1["no_data"]) == (2, 1, 1, 1), s1
    rel = sorted(ef.relpath for ef in db.list_extra_files(book_id))
    print("data files:", rel)
    assert rel == ["data/kindle-reading/x.azw3f", "data/kindle-reading/x.azw3r"], rel

    s2 = core.backup_books(db, kindle, books)
    print("second:", s2)
    assert (s2["files"], s2["unchanged"]) == (0, 2), s2

    with open(os.path.join(sdr, "x.azw3f"), "wb") as f:
        f.write(b"timer, read more")
    s3 = core.backup_books(db, kindle, books)
    assert (s3["files"], s3["unchanged"]) == (1, 1), s3
    path = [ef.file_path for ef in db.list_extra_files(book_id) if ef.relpath.endswith("azw3f")][0]
    assert open(path, "rb").read() == b"timer, read more"
    print("changed file re-copied: ok")

    logs = os.path.join(tmp, "logs")
    os.makedirs(logs)
    for day in ["2026-09-%02d" % d for d in range(1, 31)] + ["2026-10-01"]:
        os.makedirs(os.path.join(logs, day))
    out = core.backup_stats_log(kindle, logs, today="2026-10-02")
    assert os.path.exists(os.path.join(out, "snapshots.jsonl"))
    assert not os.path.exists(os.path.join(out, "card.cache"))
    kept = sorted(os.listdir(logs))
    assert len(kept) == 30 and kept[-1] == "2026-10-02" and kept[0] == "2026-09-03", kept[:3]
    print("stats log copy + keep 30: ok")
    print("ALL OK")
finally:
    shutil.rmtree(tmp)
