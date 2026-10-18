"""GUI side: run the backup whenever a Kindle's book list has been read."""

import io
import os
import shutil
import tempfile
import threading
import traceback
from functools import partial

from calibre.gui2 import FunctionDispatcher, info_dialog
from calibre.gui2.actions import InterfaceAction
from calibre.gui2.device import device_signals
from calibre.utils.config import config_dir

from calibre_plugins.inksights_backup import core

LOG_BACKUPS = os.path.join(config_dir, "plugins", "InkSights Backup", "reading-stats")
WIRELESS_APP = "KindleCalibreWireless"   # appName sent by github.com/dq-stack/CalibreWireless


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
        """(prefix, None) for a USB Kindle, (None, driver) for one connected
        through the Calibre Wireless app, or None."""
        dm = self.gui.device_manager
        dev = dm.connected_device if dm.is_device_connected else None
        if dev is None:
            return None
        if type(dev).__name__.upper().startswith("KINDLE"):
            prefix = getattr(dev, "_main_prefix", None)
            return (prefix, None) if prefix else None
        if getattr(dev, "client_app_name", None) == WIRELESS_APP:
            return (None, dev)
        return None

    def start(self, announce):
        found = self.kindle()
        if not found or self._running:
            return bool(found)
        try:
            booklist = self.gui.booklists()[0]
        except Exception:
            return False
        books = [(b.lpath, getattr(b, "application_id", None)) for b in booklist]
        db = self.gui.current_db.new_api
        self._running = True
        prefix, wireless = found
        if wireless is None:
            self._spawn(db, prefix, books, announce)
        else:
            # Reading files over Wi-Fi must happen on calibre's device thread.
            dest = tempfile.mkdtemp(prefix="inksights-")
            self.gui.device_manager.create_job(
                self._fetch_wireless,
                FunctionDispatcher(partial(self._fetched, db, dest, books, announce)),
                "InkSights Backup: reading Kindle data over Wi-Fi",
                args=[wireless, [lp for lp, _ in books], dest])
        return True

    def _fetch_wireless(self, dev, lpaths, dest):
        def get(lpath):
            buf = io.BytesIO()
            dev.get_file(lpath, buf, end_session=False)
            return buf.getvalue()
        core.mirror_over_wifi(get, lpaths, dest)

    def _fetched(self, db, dest, books, announce, job):
        if job.failed:
            shutil.rmtree(dest, ignore_errors=True)
            if isinstance(job.exception, core.OldWirelessClient):
                summary = {"crash": "", "message": "InkSights Backup needs Calibre Wireless 1.1.0 or "
                           "newer on the Kindle to back up over Wi-Fi (or connect by USB)."}
            else:
                summary = {"crash": getattr(job, "_details", "") or repr(job.exception)}
            self._done(summary, announce)
            return
        self._spawn(db, dest + os.sep, books, announce, cleanup=dest)

    def _spawn(self, db, prefix, books, announce, cleanup=None):
        threading.Thread(target=self._work, args=(db, prefix, books, announce, cleanup),
                         name="KindleReadingBackup", daemon=True).start()

    def _work(self, db, prefix, books, announce, cleanup=None):
        try:
            summary = core.backup_books(db, prefix, books)
            os.makedirs(LOG_BACKUPS, exist_ok=True)
            summary["log_copy"] = core.backup_stats_log(prefix, LOG_BACKUPS)
        except Exception:
            summary = {"crash": traceback.format_exc()}
        finally:
            if cleanup:
                shutil.rmtree(cleanup, ignore_errors=True)
        self.done(summary, announce)

    def _done(self, summary, announce):
        self._running = False
        self._last = summary
        if "crash" in summary:
            msg = summary.get("message") or "InkSights Backup failed (see calibre's debug log)"
            if summary["crash"]:
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
