#!/bin/sh
# Name: Stats Probe
# DontUseFBInk
#
# One-off device test for kindle-stats (round 4: does Python run?).
# Tap it, wait ~30 s (the first run compiles bytecode), plug in.
# Results go to /mnt/us/stats-probe/report4.txt.

OUT="/mnt/us/stats-probe/report4.txt"
PY="/mnt/us/python3/bin/python3.9"

if [ "$1" != "run" ]; then
    sh "$0" run >/dev/null 2>&1 &
    exit 0
fi

mkdir -p /mnt/us/stats-probe
{
    echo "Stats Probe 4 $(date)"
    ls -l "$PY"
    for attempt in 1 2; do
        echo "--- attempt $attempt"
        START=$(date +%s)
        "$PY" - <<'EOF'
import sys, time, sqlite3, datetime
t = time.time()
print("python", sys.version.split()[0])
from PIL import Image, ImageDraw, ImageFont
f = ImageFont.truetype("/usr/java/lib/fonts/Amazon-Ember-Bold.ttf", 30)
im = Image.new("L", (300, 60), 255)
ImageDraw.Draw(im).text((10, 10), "Hello", font=f, fill=0)
im.save("/mnt/us/stats-probe/pil-test.png")
print("PIL ok", Image.__version__ if hasattr(Image, "__version__") else "")
try:
    from _fbink import ffi, lib as fbink
    print("fbink bindings ok", ffi.string(fbink.fbink_version()).decode())
except Exception as e:
    print("fbink bindings FAIL", e)
try:
    import libevdev
    print("libevdev ok")
except Exception as e:
    print("libevdev FAIL", e)
con = sqlite3.connect("file:/var/local/cc.db?mode=ro", uri=True)
print("cc.db items", con.execute("select count(*) from Entries where p_type='Entry:Item'").fetchone()[0])
print("now local", datetime.datetime.now().isoformat(timespec="seconds"))
print("import+work secs %.2f" % (time.time() - t))
EOF
        echo "exit $?  wall $(( $(date +%s) - START ))s"
    done
    echo "done $(date)"
} > "$OUT" 2>&1
