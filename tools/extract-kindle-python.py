#!/usr/bin/env python3
"""Turn NiLuJe's Kindle Python package into a python3/ folder you can copy
straight onto the Kindle (no KUAL or MRPI needed).

Download kindle-python-*.tar.xz from NiLuJe's "Snapshots" thread on
MobileRead, then:

    python3 tools/extract-kindle-python.py kindle-python-0.15.N-r18981.tar.xz [out-dir]

and copy the resulting python3 folder to the root of the Kindle's USB drive
(so it ends up at /mnt/us/python3). Picks the build for the Paperwhite 2 and
newer ("pw2_and_up"); pass --touch for the Kindle Touch / Paperwhite 1.

What the official installer does that this skips: pre-compiling the
standard library (Python does that itself on first use, so the first launch
is a bit slower) and adding /usr/bin/python3 shortcuts (InkSights calls
Python by its full path).
"""

import io
import lzma
import os
import sys
import tarfile


def demunge(data):
    # Kindle update packages are "munged": every byte XORed with 0x7A, then
    # its nibbles swapped. Undo that with a 256-entry lookup table.
    table = bytes((((b ^ 0x7A) >> 4) | ((b ^ 0x7A) << 4)) & 0xFF for b in range(256))
    return data.translate(table)


def payload_from_bin(data):
    """The gzipped tarball inside a Kindle .bin update package."""
    plain = demunge(data[:8192])
    i = plain.find(b"\x1f\x8b\x08")
    if i < 0:
        raise SystemExit("couldn't find the payload in this .bin (unknown package format)")
    return io.BytesIO(demunge(data[i:]))


def main(argv):
    args = [a for a in argv[1:] if not a.startswith("--")]
    if not args:
        print(__doc__)
        return 2
    want = "install_touch_pw" if "--touch" in argv else "install_pw2_and_up"
    out = os.path.abspath(args[1] if len(args) > 1 else ".")

    with tarfile.open(args[0], "r:xz") as outer:
        names = [n for n in outer.getnames() if "Update_python3_" in n and want in n]
        if not names:
            raise SystemExit("no Update_python3_*%s.bin in %s" % (want, args[0]))
        print("using", os.path.basename(names[0]))
        data = outer.extractfile(names[0]).read()

    with tarfile.open(fileobj=payload_from_bin(data), mode="r:gz") as update:
        inner = update.extractfile("python3.tar.xz").read()
        version = update.extractfile("VERSION").read() if "VERSION" in update.getnames() else b""

    with tarfile.open(fileobj=io.BytesIO(lzma.decompress(inner)), mode="r:") as py:
        members = [m for m in py.getmembers() if m.name.startswith("python3/")]
        bad = [m.name for m in members if m.issym() or m.islnk() or ".." in m.name.split("/")]
        if bad:
            raise SystemExit("unexpected links/paths in the package: %s" % bad[:3])
        py.extractall(out, members=members)
    if version:
        with open(os.path.join(out, "python3", "VERSION"), "wb") as f:
            f.write(version)
    print("done:", os.path.join(out, "python3"))
    print("copy that folder to the root of the Kindle's USB drive")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
