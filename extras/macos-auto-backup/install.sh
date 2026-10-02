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

# macOS won't let a background shell read USB drives. Wrap the script in a
# tiny app so the permission ("access files on a removable volume") is asked
# for once and applies to this app only, not to every shell script.
APPLET="$APP/Kindle Backup.app"
rm -rf "$APPLET"
osacompile -o "$APPLET" -e "do shell script \"/bin/sh '$APP/kindle-backup.sh'\""
/usr/libexec/PlistBuddy -c "Add :LSUIElement bool true" "$APPLET/Contents/Info.plist"   # no Dock icon
/usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $LABEL" "$APPLET/Contents/Info.plist" 2>/dev/null \
    || /usr/libexec/PlistBuddy -c "Add :CFBundleIdentifier string $LABEL" "$APPLET/Contents/Info.plist"
codesign --force --sign - "$APPLET" 2>/dev/null
cat > "$PLIST" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>$LABEL</string>
    <key>ProgramArguments</key>
    <array><string>/usr/bin/open</string><string>-g</string><string>$APPLET</string></array>
    <key>StartOnMount</key><true/>
    <key>StandardErrorPath</key><string>$HOME/Library/Logs/kindle-backup.log</string>
</dict>
</plist>
PL
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "Installed. Backups go to ~/Documents/Kindle Backups/, log: ~/Library/Logs/kindle-backup.log"
echo "The first time a Kindle is plugged in, macOS asks to let \"Kindle Backup\" access"
echo "files on a removable volume: click Allow."
