"""Calibre plugin: back up Kindle reading data into your Calibre library."""

from calibre.customize import InterfaceActionBase


class ReadingStatsBackupPlugin(InterfaceActionBase):
    name = "Kindle Reading Backup"
    description = ("When a Kindle connects, copy each book's reading data (position, "
                   "highlights, reading timer) into that book's data files in your "
                   "library, and keep dated copies of the Reading Stats log. "
                   "Only reads from the Kindle.")
    supported_platforms = ["windows", "osx", "linux"]
    author = "dq-stack"
    version = (1, 0, 0)
    minimum_calibre_version = (6, 18, 0)     # per-book data files
    actual_plugin = "calibre_plugins.reading_stats_backup.action:BackupAction"
