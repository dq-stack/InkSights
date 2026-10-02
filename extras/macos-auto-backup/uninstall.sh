#!/bin/sh
# Remove the Kindle auto-backup (your existing backups are left alone).
LABEL="io.github.dq-stack.kindle-backup"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
rm -f "$HOME/Library/LaunchAgents/$LABEL.plist"
rm -rf "$HOME/Library/Application Support/KindleBackup"
echo "Uninstalled. Backups in ~/Documents/Kindle Backups/ were kept."
