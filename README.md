# InkSights

A Steam-style reading stats card for jailbroken Kindles, for books read in
the stock Kindle reader. Tap **InkSights** in your Library and a card
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
extensions/inksights/            (from src/ + packaging/extension/icon.png)
documents/InkSights.sh           (from packaging/documents/)
```

`tools/deploy.sh` does this from a checkout on a Mac or Linux machine.

## How the numbers work

The Kindle keeps a running total of reading time per book in each book's
sidecar folder (`<book>.sdr`), but no dates. Each time you open Reading
Stats it compares those totals with the last ones it saw and logs the
difference, split across days using the book's page-turn timestamps where
there are any. Streaks, the heatmap and the month chart come from that log
(`reading-stats/snapshots.jsonl`), so:

- history starts when you first open InkSights (time read before then
  counts toward totals but isn't dated, unless the Kindle kept page-turn
  times for it);
- re-sending a book from Calibre can reset its sidecar, but the hours already
  logged are kept;
- progress (%) is the Kindle's own figure, the same as the badge on the cover.

The card is never shown with out-of-date numbers: a saved copy is only reused
when nothing it was made from has changed.

## Backing up

Everything InkSights knows lives in **one folder on the Kindle:
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

**After a factory reset or wipe:** reinstall InkSights, then copy your
backed-up `reading-stats` folder back to the root of the Kindle. Your log,
streaks and totals carry on from where they were.

**To restore one book's position/highlights:** copy its folder from
`reading-stats/sidecars/` back to the same place under `documents/`, before
opening the book. This is only safe if the book file itself hasn't changed
(a re-converted book may not line up).

### Automatic backups with calibre

If you manage your books with [calibre](https://calibre-ebook.com) (6.18 or
newer), the **InkSights Backup** plugin backs up your reading data every
time the Kindle connects:

- each book's reading data (position, highlights, reading timer) is saved in
  that book's **data files** in your calibre library (right-click a book >
  Manage data files > `kindle-reading/`), so it moves with the book and is
  included in any backup of your library;
- the InkSights log is copied to calibre's settings folder
  (`plugins/InkSights Backup/reading-stats/<date>/`), newest 30 kept.

It only reads from the Kindle. Install `inksights-backup-calibre.zip` from the
release: calibre > Preferences > Plugins > Load plugin from file. Runs by
itself; there's also a **InkSights Backup** button (add it under
Preferences > Toolbars & menus) to run it on demand.

To restore a book's reading data, open its data files in calibre and copy the
files from `kindle-reading/` into that book's `.sdr` folder on the Kindle
(same caveat as above: only if the book file hasn't changed).

## Licence and credits

GPLv3 (see `LICENSE`), because the sidecar decoder `src/krds.py` is GPLv3.

- `krds.py`: John Howell's Kindle reader data store decoder, as patched in
  [zevisvei/kindle-reading-dashboard](https://github.com/zevisvei/kindle-reading-dashboard)
- Card design inspired by the KOReader patches of
  [quanganhdo](https://github.com/quanganhdo/koreader-user-patches) and
  [zenixlabs](https://github.com/zenixlabs/koreader-frankenpatches-public)
- Drawing via [FBInk](https://github.com/NiLuJe/FBInk) and Python from
  NiLuJe's Kindle packages
