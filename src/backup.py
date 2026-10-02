"""Keep copies of every book's sidecar files in the app's own folder.

A sidecar holds a book's reading position, highlights and reading timer.
Calibre re-sends and factory resets can delete them; the copies in
/mnt/us/reading-stats/sidecars/ survive a re-send, and copying the whole
reading-stats folder to a computer covers a reset.

Originals are only read. A copy is refreshed when the original's size or
modification time differs; copies of books that have since disappeared are
kept (that's the point).
"""

import os
import shutil

from fingerprint import SIDECAR_EXTS, is_book_sdr


def mirror_sidecars(documents, dest):
    """Copy new/changed sidecar files under dest/<path relative to documents>.
    Returns the number of files copied."""
    copied = 0
    for root, dirs, _files in os.walk(documents):
        for d in dirs:
            if not is_book_sdr(d):
                continue
            src_dir = os.path.join(root, d)
            out_dir = os.path.join(dest, os.path.relpath(src_dir, documents))
            for name in os.listdir(src_dir):
                if not name.lower().endswith(SIDECAR_EXTS):
                    continue
                src = os.path.join(src_dir, name)
                out = os.path.join(out_dir, name)
                try:
                    st = os.stat(src)
                    try:
                        o = os.stat(out)
                        # FAT keeps mtimes to 2 s, so allow that much slack
                        if o.st_size == st.st_size and abs(o.st_mtime - st.st_mtime) <= 2:
                            continue
                    except OSError:
                        pass
                    if not os.path.isdir(out_dir):
                        os.makedirs(out_dir)
                    tmp = out + ".tmp"
                    shutil.copy2(src, tmp)
                    os.replace(tmp, out)
                    copied += 1
                except OSError:
                    continue      # one unreadable file must not stop the rest
    return copied
