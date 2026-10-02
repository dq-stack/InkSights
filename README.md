# Reading Stats

A Steam-style reading stats card for jailbroken Kindles, for books read in
the stock Kindle reader. Tap **Reading Stats** in your Library and a card
pops up over it:

- **Today**, your **streak** (days with 5+ minutes) and **total** reading time
- **Hours by month** for the current year
- **Most read** books with time spent and progress
- A **26-week heatmap** of your reading days

Tap anywhere to close it. The menu bar stays live, so Home, Settings and
Search work (and close the card). Putting the Kindle to sleep closes it too.

The numbers come from the Kindle's own reading timer and library database.
Kindle system files are only ever **read**, never changed.

## Requirements

- A jailbroken Kindle with Scriptlet support (`.sh` files in `documents`
  show up in the Library). Developed and tested on a Paperwhite 2
  (FW 5.12.2.2).
- NiLuJe's Python 3 package in `/mnt/us/python3`. It's normally installed
  with MRPI; it can also be unpacked by hand (see below).

## Install

Copy onto the Kindle over USB:

```
extensions/reading-stats/        (from src/ + packaging/extension/icon.png)
documents/Reading Stats.sh       (from packaging/documents/)
```

`tools/deploy.sh` does this from a checkout on a Mac or Linux machine.

## How the numbers work

The Kindle keeps a running total of reading time per book in each book's
sidecar folder (`<book>.sdr`), but no dates. Each time you open Reading
Stats it compares those totals with the last ones it saw and logs the
difference, split across days using the book's page-turn timestamps where
there are any. Streaks, the heatmap and the month chart come from that log
(`reading-stats/snapshots.jsonl`), so:

- history starts when you first open Reading Stats (time read before then
  counts toward totals but isn't dated, unless the Kindle kept page-turn
  times for it);
- re-sending a book from Calibre can reset its sidecar, but the hours already
  logged are kept;
- progress (%) is the Kindle's own figure, the same as the badge on the cover.

The card is never shown with out-of-date numbers: a saved copy is only reused
when nothing it was made from has changed.

## Backing up

Everything Reading Stats knows lives in **one folder on the Kindle:
`reading-stats/`** (visible over USB):

| | |
|---|---|
| `snapshots.jsonl` | your reading log: every bit of reading time, by day and book |
| `sidecars/` | copies of each book's sidecar files (position, highlights, timer), refreshed whenever your reading data changes |
| `stats.log` | what the app did, for troubleshooting |

`sidecars/` keeps copies even after a book's own sidecar is deleted (for
example by a Calibre re-send), so a book's position and highlights can be
put back.

**To back up:** copy the `reading-stats` folder to your computer now and then.

**After a factory reset or wipe:** reinstall Reading Stats, then copy your
backed-up `reading-stats` folder back to the root of the Kindle. Your log,
streaks and totals carry on from where they were.

**To restore one book's position/highlights:** copy its folder from
`reading-stats/sidecars/` back to the same place under `documents/`, before
opening the book. This is only safe if the book file itself hasn't changed
(a re-converted book may not line up).

### Optional: automatic backups on a Mac

`extras/macos-auto-backup/` backs up the Kindle every time it's plugged into
your Mac: `reading-stats/` plus every book's sidecar folder, into
`~/Documents/Kindle Backups/<date>/`, keeping the newest 30. It only reads
from the Kindle.

```
sh extras/macos-auto-backup/install.sh      # set up (no admin rights needed)
sh extras/macos-auto-backup/uninstall.sh    # remove (keeps your backups)
```

Log: `~/Library/Logs/kindle-backup.log`. macOS doesn't let background jobs
read USB drives, so the installer wraps the script in a tiny invisible app,
"Kindle Backup". The first time it runs, macOS may ask whether it can access
files on a removable volume: click Allow. (If it doesn't ask and the log says
it can't read the Kindle, allow it under System Settings > Privacy & Security
> Files and Folders.)

## Licence and credits

GPLv3 (see `LICENSE`), because the sidecar decoder `src/krds.py` is GPLv3.

- `krds.py`: John Howell's Kindle reader data store decoder, as patched in
  [zevisvei/kindle-reading-dashboard](https://github.com/zevisvei/kindle-reading-dashboard)
- Card design inspired by the KOReader patches of
  [quanganhdo](https://github.com/quanganhdo/koreader-user-patches) and
  [zenixlabs](https://github.com/zenixlabs/koreader-frankenpatches-public)
- Drawing via [FBInk](https://github.com/NiLuJe/FBInk) and Python from
  NiLuJe's Kindle packages
