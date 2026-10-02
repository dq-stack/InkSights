#!/bin/sh
# Build the calibre plugin zip (install it via calibre: Preferences > Plugins >
# Load plugin from file, or `calibre-customize -a <zip>`).
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
OUT="${1:-$ROOT/release/inksights-backup-calibre.zip}"
mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
cd "$ROOT/calibre-plugin"
zip -q -X "$OUT" __init__.py action.py core.py plugin-import-name-inksights_backup.txt images/icon.png
echo "$OUT"
