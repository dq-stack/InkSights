#!/bin/sh
# Build the release downloads:
#   release/inksights-<version>.zip               copy onto the Kindle's USB drive
#   release/inksights-backup-calibre-<version>.zip calibre plugin
# Needs Go 1.23 (1.24+ dropped support for the Linux kernels older Kindles run).
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${1:-${VERSION:-dev}}"
STAGE="$ROOT/build/package"
OUT="$ROOT/release"
APP="$STAGE/extensions/inksights"
rm -rf "$STAGE"
mkdir -p "$APP/bin" "$STAGE/documents" "$OUT"
(cd "$ROOT/app" && GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$APP/bin/inksights" ./cmd/inksights)
cp "$ROOT/packaging/extension/activity.sh" "$ROOT/packaging/extension/icon.png" "$ROOT/LICENSE" "$APP/"
cp "$ROOT/packaging/documents/InkSights.sh" "$STAGE/documents/"
echo "$VERSION" > "$APP/VERSION"
rm -f "$OUT/inksights-$VERSION.zip"
(cd "$STAGE" && zip -q -r -X "$OUT/inksights-$VERSION.zip" extensions documents)
sh "$ROOT/tools/build-calibre-plugin.sh" "$OUT/inksights-backup-calibre-$VERSION.zip" >/dev/null
ls -1 "$OUT"/*"$VERSION"*.zip
