#!/bin/sh
# Install the Kindle auto-backup for the current user (no admin rights needed).
set -eu
HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
LABEL="io.github.dq-stack.kindle-backup"
APP="$HOME/Library/Application Support/KindleBackup"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

mkdir -p "$APP" "$HOME/Library/LaunchAgents" "$HOME/Library/Logs"
cp "$HERE/kindle-backup.sh" "$APP/kindle-backup.sh"
chmod +x "$APP/kindle-backup.sh"
cat > "$PLIST" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>$LABEL</string>
    <key>ProgramArguments</key>
    <array><string>/bin/sh</string><string>$APP/kindle-backup.sh</string></array>
    <key>StartOnMount</key><true/>
    <key>StandardErrorPath</key><string>$HOME/Library/Logs/kindle-backup.log</string>
</dict>
</plist>
PL
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "Installed. Backups go to ~/Documents/Kindle Backups/, log: ~/Library/Logs/kindle-backup.log"
