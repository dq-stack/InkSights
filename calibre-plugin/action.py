"""GUI side: run the backup whenever a Kindle's book list has been read."""

import os
import threading
import traceback

from calibre.gui2 import FunctionDispatcher, info_dialog
from calibre.gui2.actions import InterfaceAction
from calibre.gui2.device import device_signals
from calibre.utils.config import config_dir

from calibre_plugins.inksights_backup import core

LOG_BACKUPS = os.path.join(config_dir, "plugins", "InkSights Backup", "reading-stats")


class BackupAction(InterfaceAction):
    name = "InkSights Backup"
    action_spec = ("InkSights Backup", None,
                   "Back up Kindle reading data into the library now", None)
    action_type = "current"
    dont_add_to = frozenset(["context-menu", "context-menu-device"])

    def genesis(self):
        self._running = False
        self._last = None
        self.qaction.setIcon(get_icons("images/icon.png", "InkSights Backup"))  # noqa: F821 (calibre builtin)
        self.qaction.triggered.connect(self.backup_now)
        self.done = FunctionDispatcher(self._done)
        device_signals.device_metadata_available.connect(self.backup_auto)

    # --- triggers ---

    def backup_auto(self):
        self.start(announce=False)

    def backup_now(self):
        if not self.start(announce=True):
            info_dialog(self.gui, "InkSights Backup",
                        "Connect your Kindle first (and wait for calibre to read its books).",
                        show=True)

    # --- work ---

    def kindle(self):
        dm = self.gui.device_manager
        dev = dm.connected_device if dm.is_device_connected else None
        if dev is None or not type(dev).__name__.upper().startswith("KINDLE"):
            return None
        return getattr(dev, "_main_prefix", None)

    def start(self, announce):
        prefix = self.kindle()
        if not prefix or self._running:
            return bool(prefix)
        try:
            booklist = self.gui.booklists()[0]
        except Exception:
            return False
        books = [(b.lpath, getattr(b, "application_id", None)) for b in booklist]
        db = self.gui.current_db.new_api
        self._running = True
        threading.Thread(target=self._work, args=(db, prefix, books, announce),
                         name="KindleReadingBackup", daemon=True).start()
        return True

    def _work(self, db, prefix, books, announce):
        try:
            summary = core.backup_books(db, prefix, books)
            os.makedirs(LOG_BACKUPS, exist_ok=True)
            summary["log_copy"] = core.backup_stats_log(prefix, LOG_BACKUPS)
        except Exception:
            summary = {"crash": traceback.format_exc()}
        self.done(summary, announce)

    def _done(self, summary, announce):
        self._running = False
        self._last = summary
        if "crash" in summary:
            msg = "InkSights Backup failed (see calibre's debug log)"
            print(summary["crash"])
        else:
            msg = "InkSights Backup: %d file(s) from %d book(s) saved, %d unchanged" % (
                summary["files"], summary["books"], summary["unchanged"])
            if summary["unmatched"]:
                msg += ", %d Kindle book(s) not in this library" % summary["unmatched"]
            for e in summary["errors"]:
                print("InkSights Backup:", e)
        self.gui.status_bar.show_message(msg, 8000)
        if announce:
            details = msg
            if summary.get("log_copy"):
                details += "\n\nInkSights reading log copied to:\n" + summary["log_copy"]
            info_dialog(self.gui, "InkSights Backup", details, show=True)
