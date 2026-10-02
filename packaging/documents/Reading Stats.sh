#!/bin/sh
# Name: Reading Stats
# Icon: /mnt/us/extensions/reading-stats/icon.png
# DontUseFBInk

APP="/mnt/us/extensions/reading-stats"
PY="/mnt/us/python3/bin/python3.9"

if [ ! -x "$PY" ]; then
    eips 2 30 "Reading Stats needs Python 3 in /mnt/us/python3"
    exit 1
fi

mkdir -p /mnt/us/reading-stats

# Run ahead of the Kindle's own background work (saving reading position,
# library updates) so the card isn't kept waiting. Skipped if not allowed.
BOOST=""
if nice -n -10 true 2>/dev/null; then
    BOOST="nice -n -10"
    if command -v ionice >/dev/null 2>&1 && ionice -c 2 -n 0 true 2>/dev/null; then
        BOOST="$BOOST ionice -c 2 -n 0"
    fi
fi
# Return straight away so the Library isn't blocked; the card closes itself.
RS_LAUNCHED=$(date +%s) DISPLAY=:0 $BOOST "$PY" -S "$APP/main.py" >> /mnt/us/reading-stats/launch.log 2>&1 &
exit 0
