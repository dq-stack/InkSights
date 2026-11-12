// Package backup keeps copies of every book's sidecar files in the app's
// own folder.
//
// A sidecar holds a book's reading position, highlights and reading timer.
// Calibre re-sends and factory resets can delete them; the copies in
// /mnt/us/reading-stats/sidecars/ survive a re-send, and copying the whole
// reading-stats folder to a computer covers a reset.
//
// Originals are only read. A copy is refreshed when the original's size or
// modification time differs; copies of books that have since disappeared
// are kept (that's the point).
package backup

import (
	"io"
	"os"
	"path/filepath"

	"github.com/dq-stack/InkSights/app/internal/books"
)

// MirrorSidecars copies new or changed sidecar files to dest/<path relative
// to documents> and returns how many it copied.
func MirrorSidecars(documents, dest string) int {
	copied := 0
	for _, sdr := range books.BookSdrs(documents) {
		rel, err := filepath.Rel(documents, sdr)
		if err != nil {
			continue
		}
		outDir := filepath.Join(dest, rel)
		for _, name := range books.Sidecars(sdr) {
			src, out := filepath.Join(sdr, name), filepath.Join(outDir, name)
			st, err := os.Stat(src)
			if err != nil {
				continue
			}
			if o, err := os.Stat(out); err == nil && o.Size() == st.Size() {
				// FAT keeps modification times to 2 s, so allow that much slack
				d := o.ModTime().Sub(st.ModTime())
				if d < 0 {
					d = -d
				}
				if d.Seconds() <= 2 {
					continue
				}
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				continue
			}
			if copyFile(src, out+".tmp", st) == nil && os.Rename(out+".tmp", out) == nil {
				copied++
			}
		}
	}
	return copied
}

func copyFile(src, dst string, st os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Chtimes(dst, st.ModTime(), st.ModTime())
}
