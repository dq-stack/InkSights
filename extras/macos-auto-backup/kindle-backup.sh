#!/bin/sh
# Back up a Kindle's reading data whenever it's plugged into this Mac.
# Run by launchd on every volume mount; does nothing unless a Kindle is there.
# Only reads from the Kindle.
#
# Copies to ~/Documents/Kindle Backups/YYYY-MM-DD/:
#   reading-stats/   the Reading Stats app's folder (stats log + sidecar copies)
#   documents/       every book's sidecar folder (*.sdr: position, highlights)
# One folder per day (a later plug-in that day updates it); keeps the newest 30.

KINDLE="${KINDLE:-/Volumes/Kindle}"
DEST_ROOT="${KINDLE_BACKUP_DIR:-$HOME/Documents/Kindle Backups}"
KEEP=30
LOG="$HOME/Library/Logs/kindle-backup.log"

log() { echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> "$LOG"; }

# launchd fires as the volume appears; give the mount a moment to settle
sleep 5
[ -d "$KINDLE/documents" ] || exit 0

if ! ls "$KINDLE/documents" >/dev/null 2>&1; then
    log "can't read $KINDLE: allow \"Kindle Backup\" in System Settings > Privacy & Security > Files and Folders (Removable Volumes)"
    exit 1
fi

DEST="$DEST_ROOT/$(date +%Y-%m-%d)"
mkdir -p "$DEST" || { log "cannot create $DEST"; exit 1; }

if [ -d "$KINDLE/reading-stats" ]; then
    rsync -rt --exclude '._*' --exclude 'card.cache' "$KINDLE/reading-stats/" "$DEST/reading-stats/" \
        || log "rsync reading-stats failed ($?)"
fi
rsync -rt --exclude '._*' --include '*/' --include '**.sdr/**' --exclude '*' --prune-empty-dirs \
    "$KINDLE/documents/" "$DEST/documents/" || log "rsync documents failed ($?)"

# keep the newest $KEEP dated folders
ls -1d "$DEST_ROOT"/20??-??-?? 2>/dev/null | sort -r | tail -n +$((KEEP + 1)) | while read -r old; do
    rm -rf "$old" && log "removed old backup $old"
done

log "backed up to $DEST ($(du -sh "$DEST" | cut -f1))"
