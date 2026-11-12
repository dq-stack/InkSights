#!/bin/sh
#
# InkSights wake/sleep recorder.
#
# Writes one line to /mnt/us/reading-stats/activity.log each time the Kindle
# wakes or sleeps (and which book was open at sleep). InkSights uses it to put
# reading time on the day it happened (see app/internal/activity). It does nothing else
# and sits idle in lipc-wait-event between events.
#
# InkSights starts it from a copy in /tmp, in its own session, the first time
# InkSights is opened after a restart. Nothing here is held open on /mnt/us:
# USB drive mode kills processes using /mnt/us, and each line is written with
# a short append.
#

LOG="/mnt/us/reading-stats/activity.log"

cd / || exit 1
echo $$ > /tmp/inksights-activity.pid

note() {
    echo "$(date '+%Y-%m-%dT%H:%M:%S') $*" >> "$LOG" 2>/dev/null
}

#
# The book open in the reader, if any. The KF8 renderer (webreader) holds
# exactly that book's file open. One grep over every process name, because a
# per-process loop takes seconds on the Kindle's CPU. Same method as CoverSleep.
#
open_book() {
    for COMM in $(grep -l -x webreader /proc/[0-9]*/comm 2>/dev/null); do
        ls -l "${COMM%/comm}/fd" 2>/dev/null |
            sed -n 's/.*-> //p' |
            grep -E '^/mnt/(us|base-us)/documents/'
    done | head -n 1
}

in_reader() {
    [ "$(lipc-get-prop com.lab126.appmgrd activeApp 2>/dev/null)" = "com.lab126.booklet.reader" ]
}

note start

lipc-wait-event -m com.lab126.powerd goingToScreenSaver,outOfScreenSaver 2>/dev/null |
while read -r EVENT _; do
    case "$EVENT" in
        outOfScreenSaver)
            note wake
            ;;
        goingToScreenSaver)
            BOOK="$(open_book)"
            if [ -n "$BOOK" ]; then
                note sleep book "$BOOK"
            elif in_reader; then
                note sleep reader
            else
                note sleep library
            fi
            ;;
    esac
done

note stop
rm -f /tmp/inksights-activity.pid
