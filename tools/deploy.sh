#!/bin/sh
# Build the checkout and copy it onto a Kindle, over USB (default:
# /Volumes/Kindle, or $KINDLE) or over SSH (KINDLE_SSH=root@<ip>).
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
STAGE="$ROOT/build/deploy"
rm -rf "$STAGE"
mkdir -p "$STAGE/extensions/inksights/bin" "$STAGE/documents"
(cd "$ROOT/app" && GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$(git -C "$ROOT" describe --always --dirty)" \
    -o "$STAGE/extensions/inksights/bin/inksights" ./cmd/inksights)
cp "$ROOT/packaging/extension/activity.sh" "$ROOT/packaging/extension/icon.png" "$ROOT/LICENSE" \
    "$STAGE/extensions/inksights/"
cp "$ROOT/packaging/documents/InkSights.sh" "$STAGE/documents/"

if [ -n "${KINDLE_SSH:-}" ]; then
    # Only rewrite the Library tile when it changed: the Kindle re-indexes a
    # rewritten .sh as a new item, which drops it out of its collection.
    (cd "$STAGE" && COPYFILE_DISABLE=1 tar -cf - extensions) |
        ssh "$KINDLE_SSH" 'cd /mnt/us && tar -xf - && chmod +x extensions/inksights/bin/inksights extensions/inksights/activity.sh'
    ssh "$KINDLE_SSH" 'cat /mnt/us/documents/InkSights.sh 2>/dev/null' | cmp -s - "$STAGE/documents/InkSights.sh" ||
        ssh "$KINDLE_SSH" 'cat > /mnt/us/documents/InkSights.sh' < "$STAGE/documents/InkSights.sh"
    echo "Deployed $(git -C "$ROOT" describe --always --dirty) to $KINDLE_SSH"
    exit 0
fi

K="${KINDLE:-/Volumes/Kindle}"
[ -d "$K/documents" ] || { echo "Kindle not mounted at $K (or set KINDLE_SSH=root@<ip>)" >&2; exit 1; }
mkdir -p "$K/extensions/inksights/bin"
cp "$STAGE/extensions/inksights/bin/inksights" "$K/extensions/inksights/bin/"
cp "$STAGE/extensions/inksights/activity.sh" "$STAGE/extensions/inksights/icon.png" \
    "$STAGE/extensions/inksights/LICENSE" "$K/extensions/inksights/"
cmp -s "$STAGE/documents/InkSights.sh" "$K/documents/InkSights.sh" ||
    cp "$STAGE/documents/InkSights.sh" "$K/documents/InkSights.sh"
dot_clean -m "$K/extensions/inksights" "$K/documents" 2>/dev/null || true
sync
echo "Deployed $(git -C "$ROOT" describe --always --dirty) to $K"
