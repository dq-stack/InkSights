#!/bin/sh
# Copy the app onto a USB-mounted Kindle. Whatever is checked out is what
# gets deployed, so `git checkout pillow-v0 && tools/deploy.sh` reverts.
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
K="${KINDLE:-/Volumes/Kindle}"
[ -d "$K/documents" ] || { echo "Kindle not mounted at $K" >&2; exit 1; }
APP="$K/extensions/reading-stats"
mkdir -p "$APP"
rm -rf "$APP/__pycache__" "$APP"/*.py
cp "$ROOT"/src/*.py "$ROOT/packaging/extension/icon.png" "$ROOT/LICENSE" "$APP/"
cp "$ROOT/packaging/documents/Reading Stats.sh" "$K/documents/Reading Stats.sh"
rm -f "$K/reading-stats/card.cache"      # drawn by the previous build
dot_clean -m "$APP" "$K/documents" 2>/dev/null || true
sync
echo "Deployed $(git -C "$ROOT" describe --always --dirty) to $APP"
