// Package fingerprint is a cheap fingerprint of everything the card is
// computed from.
//
// If two launches have the same fingerprint, their cards are identical, so
// the saved card may be shown without recomputing. Anything that can change
// a number on the card is in here:
//
//   - today's date (Today, streak, heatmap, month chart)
//   - every sidecar file's name, size and modification time (reading time)
//   - each book's title and progress from the library database (cc.db)
//   - the reading log's size and modification time
//   - the app itself (a new version may draw differently)
//
// It only stats files and never decodes them, so it stays fast.
package fingerprint

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/clock"
)

func statLine(path, rel string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "missing " + rel + "\n"
	}
	return fmt.Sprintf("%s|%d|%d\n", rel, st.Size(), st.ModTime().UnixNano())
}

// Quick covers everything except the library database: date, sidecar file
// stats, the reading log and the app. After reading it nearly always
// differs, which lets the database query be skipped.
func Quick(documents, snapshots string, today time.Time, extra ...string) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s\n", clock.Day(today))
	for _, sdr := range books.BookSdrs(documents) {
		for _, name := range books.Sidecars(sdr) {
			p := filepath.Join(sdr, name)
			rel, _ := filepath.Rel(documents, p)
			h.Write([]byte(statLine(p, rel)))
		}
	}
	h.Write([]byte(statLine(snapshots, "snapshots")))
	for _, e := range extra {
		h.Write([]byte(statLine(e, filepath.Base(e))))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Full is Quick plus each book's title and progress from the library
// database (the read state matters: "Mark as Read" changes only that).
func Full(quick, ccdb string) string {
	h := sha1.New()
	h.Write([]byte(quick))
	rows, err := books.LibraryRows(ccdb)
	if err != nil {
		fmt.Fprintf(h, "ccdb-error %v", err)
	}
	for _, r := range rows {
		h.Write([]byte(strings.Join(r, "\x1f") + "\x1e"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
