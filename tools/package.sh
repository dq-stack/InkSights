#!/bin/sh
# Build the release downloads:
#   release/inksights-<version>.zip               copy onto the Kindle's USB drive
#   release/inksights-backup-calibre-<version>.zip calibre plugin
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${1:-${VERSION:-dev}}"
STAGE="$ROOT/build/package"
OUT="$ROOT/release"
rm -rf "$STAGE"
mkdir -p "$STAGE/extensions/inksights" "$STAGE/documents" "$OUT"
cp "$ROOT"/src/*.py "$ROOT/packaging/extension/icon.png" "$ROOT/LICENSE" "$STAGE/extensions/inksights/"
cp "$ROOT/packaging/documents/InkSights.sh" "$STAGE/documents/"
echo "$VERSION" > "$STAGE/extensions/inksights/VERSION"
rm -f "$OUT/inksights-$VERSION.zip"
(cd "$STAGE" && zip -q -r -X "$OUT/inksights-$VERSION.zip" extensions documents)
sh "$ROOT/tools/build-calibre-plugin.sh" "$OUT/inksights-backup-calibre-$VERSION.zip" >/dev/null
ls -1 "$OUT"/*"$VERSION"*.zip
