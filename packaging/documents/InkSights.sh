#!/bin/sh
# Name: InkSights
# Icon: /mnt/us/extensions/inksights/icon.png
# DontUseFBInk

APP="/mnt/us/extensions/inksights"
BIN="$APP/bin/inksights"
RUN="/tmp/inksights"

if [ ! -f "$BIN" ]; then
    eips 2 30 "InkSights: $BIN is missing"
    exit 1
fi

# Run a copy from /tmp: USB drive mode kills processes using files on /mnt/us.
if [ ! -x "$RUN" ] || [ "$BIN" -nt "$RUN" ]; then
    cp "$BIN" "$RUN.new" && chmod +x "$RUN.new" && mv "$RUN.new" "$RUN"
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
# InkSights decides: on firmware where the Library pauses while a scriptlet
# runs (5.18), it stays until the card is closed so the Library can't wake
# up and flash the screen under the card; elsewhere it carries on in the
# background and this returns straight away.
RS_LAUNCHED=$(date +%s) INKSIGHTS_FOREGROUND=1 INKSIGHTS_DIR="$APP" DISPLAY=:0 $BOOST "$RUN" >> /mnt/us/reading-stats/launch.log 2>&1
exit 0
