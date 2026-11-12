// Package books finds the books on the Kindle and reads their reading time
// (from the .sdr sidecars) and progress and titles (from the library
// database, cc.db). Everything here is read-only.
//
// Progress comes from cc.db, the same number as the % badge on each cover.
// timer.model's totalPercent is NOT progress (it's the share of the book's
// words the timer has measured), so it's never used for that.
package books

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dq-stack/InkSights/app/internal/krds"
)

// DeviceDocuments is where the Kindle's library lives (cc.db locations use it).
const DeviceDocuments = "/mnt/us/documents/"

// SidecarExts are the reading data files inside a book's .sdr folder.
var SidecarExts = []string{".azw3f", ".yjf", ".mbs", ".azw3r", ".yjr", ".mbp1"}

// Not books: the clippings text file and the jailbreak marker document.
var ignoreTitles = map[string]bool{"My Clippings": true, "JAILBROKEN": true}

// IsBookSdr reports whether a folder name is a book's sidecar folder
// (not My Clippings, the jailbreak marker or a scriptlet's .sh.sdr).
func IsBookSdr(name string) bool {
	if !strings.HasSuffix(name, ".sdr") {
		return false
	}
	base := strings.TrimSuffix(name, ".sdr")
	return !ignoreTitles[base] && !strings.HasSuffix(base, ".sh")
}

// IsSidecar reports whether a file name is a reading data file.
func IsSidecar(name string) bool {
	l := strings.ToLower(name)
	for _, e := range SidecarExts {
		if strings.HasSuffix(l, e) {
			return true
		}
	}
	return false
}

// BookSdrs returns every book sidecar folder under documents, sorted.
func BookSdrs(documents string) []string {
	var out []string
	filepath.WalkDir(documents, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p != documents && IsBookSdr(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// Sidecars returns the reading data file names in an .sdr folder, sorted.
func Sidecars(sdr string) []string {
	entries, err := os.ReadDir(sdr)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && IsSidecar(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Book is one book's reading data.
type Book struct {
	Key       string // .sdr path relative to documents ("Author/Title.sdr")
	Title     string
	Author    string
	TotalMs   int64 // timer.model total; 0 = no timer data
	Words     int64
	HasWords  bool
	Percent   *float64 // progress 0-1 from cc.db; nil = unknown
	HistoryMs []int64  // page.history.store times (ms since the epoch)
	Sidecars  []string
	Errors    []string
}

// TitleFromSdr is a best-effort display title from a sidecar folder name:
// "Title_ Subtitle - Author.sdr" (Calibre), "Title_ASIN.sdr" (Amazon) and
// Calibre's "Title, The" sort order.
func TitleFromSdr(sdr string) string {
	name := strings.TrimSuffix(filepath.Base(sdr), ".sdr")
	if i := strings.LastIndex(name, " - "); i >= 0 {
		name = name[:i]
	}
	for _, a := range []string{"The", "A", "An"} {
		if strings.HasSuffix(name, ", "+a) {
			name = a + " " + strings.TrimSuffix(name, ", "+a)
		}
	}
	if i := strings.Index(name, "_"); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// CleanTitle drops a subtitle: "Shoe Dog: A Memoir by…" -> "Shoe Dog".
func CleanTitle(t string) string {
	if i := strings.Index(t, ": "); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

func collectBook(sdr, documents string) Book {
	rel, _ := filepath.Rel(documents, sdr)
	b := Book{Key: filepath.ToSlash(rel), Title: TitleFromSdr(sdr)}
	haveTimer := false
	for _, name := range Sidecars(sdr) {
		b.Sidecars = append(b.Sidecars, name)
		data, err := os.ReadFile(filepath.Join(sdr, name))
		if err != nil {
			b.Errors = append(b.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		vals, err := krds.Decode(data)
		if err != nil {
			// partial/unknown data must never stop the run
			b.Errors = append(b.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if t, ok := krds.TimerOf(vals); ok && !haveTimer {
			haveTimer = true
			b.TotalMs = t.TotalMs
			b.Words, b.HasWords = t.TotalWords, true
		}
		b.HistoryMs = append(b.HistoryMs, krds.HistoryMs(vals)...)
	}
	return b
}

// Collect reads every book's sidecars, with titles and progress from cc.db
// when it's readable.
func Collect(documents, ccdb string) []Book {
	lib, _ := Library(ccdb)
	var out []Book
	for _, sdr := range BookSdrs(documents) {
		b := collectBook(sdr, documents)
		if m, ok := lib[b.Key]; ok {
			if m.Title != "" {
				b.Title = m.Title
			}
			b.Author = m.Author
			b.Percent = m.Percent
		}
		out = append(out, b)
	}
	return out
}

// Meta is a book's library entry.
type Meta struct {
	Title   string
	Author  string
	Percent *float64
}

// Query runs a read-only query with the Kindle's sqlite3 command and returns
// rows of columns (NULL comes back as ""). The command is used instead of a
// built-in SQLite to keep the app small; the Kindle always has it.
func Query(db, query string) ([][]string, error) {
	if _, err := os.Stat(db); err != nil {
		return nil, err
	}
	cmd := exec.Command("sqlite3", "-readonly", "-batch",
		"-cmd", ".timeout 3000", "-separator", "\x1f", "-newline", "\x1e",
		db, query)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var rows [][]string
	for _, line := range strings.Split(string(out), "\x1e") {
		line = strings.TrimPrefix(line, "\n")
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\x1f"))
	}
	return rows, nil
}

// hasColumn reports whether cc.db's Entries table has this column.
func hasColumn(db, col string) (bool, error) {
	rows, err := Query(db, "PRAGMA table_info(Entries)")
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if len(r) > 1 && r[1] == col {
			return true, nil
		}
	}
	return false, nil
}

func readStateExpr(db string) string {
	// "Mark as Read" sets p_readState to 1 but leaves p_percentFinished
	// where you stopped (5.12 and later have the column)
	if ok, _ := hasColumn(db, "p_readState"); ok {
		return "ifnull(p_readState, '')"
	}
	return "''"
}

// LibraryRows returns (location, title, author, percent, read state) for
// items in documents/, ordered by location.
func LibraryRows(db string) ([][]string, error) {
	if db == "" {
		return nil, errors.New("no library database")
	}
	q := fmt.Sprintf("SELECT p_location, ifnull(p_titles_0_nominal,''), "+
		"ifnull(p_credits_0_name_collation,''), ifnull(p_percentFinished,''), %s "+
		"FROM Entries WHERE p_type = 'Entry:Item' AND p_location LIKE '%s%%' ORDER BY p_location",
		readStateExpr(db), DeviceDocuments)
	return Query(db, q)
}

// Library maps sidecar key (relative .sdr path) to title, author and
// progress. A book marked as read counts as 100%, whatever page you stopped
// on. A missing or unreadable cc.db just means no titles/progress.
func Library(db string) (map[string]Meta, error) {
	books := map[string]Meta{}
	rows, err := LibraryRows(db)
	if err != nil {
		return books, err
	}
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		rel := strings.TrimPrefix(r[0], DeviceDocuments)
		key := strings.TrimSuffix(rel, filepath.Ext(rel)) + ".sdr"
		var pct *float64
		if r[3] != "" {
			if f, err := strconv.ParseFloat(r[3], 64); err == nil {
				v := f / 100
				pct = &v
			}
		}
		if strings.TrimSpace(r[4]) == "1" {
			v := 1.0
			pct = &v
		}
		m := Meta{Author: r[2], Percent: pct}
		if r[1] != "" {
			m.Title = CleanTitle(r[1])
		}
		books[key] = m
	}
	return books, nil
}
