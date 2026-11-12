package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dq-stack/InkSights/app/internal/activity"
	"github.com/dq-stack/InkSights/app/internal/backup"
	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/card"
	"github.com/dq-stack/InkSights/app/internal/clock"
	"github.com/dq-stack/InkSights/app/internal/fingerprint"
	"github.com/dq-stack/InkSights/app/internal/history"
	"github.com/dq-stack/InkSights/app/internal/screen"
	"github.com/dq-stack/InkSights/app/internal/stats"
	"github.com/dq-stack/InkSights/app/internal/touch"
	"github.com/dq-stack/InkSights/app/internal/xshield"
)

const (
	dataDir       = "/mnt/us/reading-stats"
	documents     = "/mnt/us/documents"
	ccdb          = "/var/local/cc.db"
	coversleepLog = "/mnt/us/extensions/custom-screensaver/custom_ss.log"
	deskLog       = "/mnt/us/desk/desk.log"
	lockPath      = "/tmp/reading-stats.lock"
	recorderTmp   = "/tmp/inksights-activity.sh"
	recorderPid   = "/tmp/inksights-activity.pid"
	cardTmp       = "/tmp/inksights-card.png"

	backstop    = 10 * time.Minute
	recheck     = 3 * time.Second // the Library refreshes ~1-2 s after a scriptlet launch
	designWidth = 758             // PW2; other screens scale from this
	fallbackTop = 116             // PW2 menu bar height if the Library window isn't found
	cardGap     = 16              // space between the menu bar and the card
	bottomGap   = 8               // space kept free below the card
	cacheVer    = 1
)

var (
	logPath     = filepath.Join(dataDir, "stats.log")
	snapshots   = filepath.Join(dataDir, "snapshots.jsonl")
	activityLog = filepath.Join(dataDir, "activity.log")
	sidecarBak  = filepath.Join(dataDir, "sidecars")
	cachePNG    = filepath.Join(dataDir, "card.png")
	cacheMeta   = filepath.Join(dataDir, "card.json")
	tStart      = time.Now()
)

// --- logging: one short append per line, so nothing on /mnt/us stays open
// (USB drive mode kills processes holding files there) ---

func logf(level, format string, a ...any) {
	line := fmt.Sprintf("%s %s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), level, fmt.Sprintf(format, a...))
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	f.WriteString(line)
	f.Close()
}

func info(format string, a ...any) { logf("INFO", format, a...) }
func warn(format string, a ...any) { logf("WARNING", format, a...) }

func since() float64 { return time.Since(tStart).Seconds() }

func setupLogging() {
	os.MkdirAll(dataDir, 0o755)
	if st, err := os.Stat(logPath); err == nil && st.Size() > 256*1024 {
		os.Rename(logPath, logPath+".1")
	}
}

func singleInstance() bool {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return true // can't lock: run anyway
	}
	// kept open (and locked) until the process exits
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

// --- wake/sleep recorder (activity.sh), started once per Kindle restart ---

func recorderRunning() bool {
	b, err := os.ReadFile(recorderPid)
	if err != nil {
		return false
	}
	cmd, err := os.ReadFile("/proc/" + strings.TrimSpace(string(b)) + "/cmdline")
	return err == nil && strings.Contains(string(cmd), "inksights-activity")
}

func startRecorder(appDir string) {
	src := filepath.Join(appDir, "activity.sh")
	if recorderRunning() {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := os.WriteFile(recorderTmp, data, 0o755); err != nil {
		warn("recorder copy failed: %v", err)
		return
	}
	// from /tmp, in its own session so it outlives this card
	cmd := exec.Command("/bin/sh", recorderTmp)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		warn("recorder didn't start: %v", err)
		return
	}
	// normal priority (this app runs boosted)
	syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 0)
	cmd.Process.Release()
	info("started wake/sleep recorder")
}

// --- events that close the card ---

// Found with a probe on a PW2 (FW 5.12): plugging into a computer sends hal
// usbPlugIn, volumd userstoreIsLikelyToUnMount and hal usbConfigured, then
// volumd userstoreUnAvailable as USB drive mode starts. usbPlugIn alone also
// fires for a wall charger, which shouldn't close it.
var closeEvents = [][3]string{
	{"com.lab126.powerd", "goingToScreenSaver", "going to sleep"},
	{"com.lab126.hal", "usbConfigured", "USB connected"},
	{"com.lab126.volumd", "userstoreIsLikelyToUnMount,userstoreUnAvailable", "USB drive mode"},
}

// watch starts one lipc-wait-event per publisher; a reason is sent only
// when one actually reports an event (a watcher that can't start, e.g. on
// firmware without that publisher, never closes the card).
func watch(reasons chan<- string) []*exec.Cmd {
	var cmds []*exec.Cmd
	for _, ev := range closeEvents {
		cmd := exec.Command("lipc-wait-event", ev[0], ev[1])
		out, err := cmd.StdoutPipe()
		if err != nil {
			continue
		}
		if err := cmd.Start(); err != nil {
			warn("can't watch %s: %v", ev[0], err)
			continue
		}
		cmds = append(cmds, cmd)
		reason := ev[2]
		go func() {
			if line, _ := bufio.NewReader(out).ReadString('\n'); strings.TrimSpace(line) != "" {
				reasons <- reason
			}
			io.Copy(io.Discard, out)
			cmd.Wait()
		}()
	}
	return cmds
}

// --- saved card: the last drawn card and where it went, so the next launch
// can show it before asking X anything (only if its fingerprint matches) ---

type cache struct {
	Ver     int    `json:"v"`
	ScreenW int    `json:"screen_w"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Top     int    `json:"top"`
	LibH    int    `json:"lib_h"`
	Quick   string `json:"fp_quick"`
	Full    string `json:"fp_full"`
}

func loadCache(screenW int) *cache {
	b, err := os.ReadFile(cacheMeta)
	if err != nil {
		return nil
	}
	var c cache
	if json.Unmarshal(b, &c) != nil || c.Ver != cacheVer || c.ScreenW != screenW {
		return nil
	}
	if _, err := os.Stat(cachePNG); err != nil {
		return nil
	}
	return &c
}

func saveCache(c cache) {
	c.Ver = cacheVer
	b, _ := json.Marshal(c)
	if copyTo(cardTmp, cachePNG) != nil {
		return
	}
	os.WriteFile(cacheMeta+".tmp", b, 0o644)
	os.Rename(cacheMeta+".tmp", cacheMeta)
}

func copyTo(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst+".tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(dst+".tmp", dst)
}

// --- layout ---

// libraryArea is the (top, height) of the Library's content, between its
// top menu/search bar and any bottom bar, so those stay usable.
func libraryArea(xd *xshield.Display, w, h int) (int, int) {
	if xd != nil {
		// Firmware up to 5.15 or so: the Library is the home booklet. Match
		// the application window, not the Library's "⋮" menu dialog, which
		// shares the booklet id.
		if _, y, _, ht, ok := xd.FindWindow("N:application_ID:com.lab126.booklet.home"); ok && y > 0 {
			return y, ht
		}
		// Newer firmware (seen on 5.18): the Library is KPPMainApp, with a
		// search bar window over its top and a bottom bar over its foot.
		if _, y, _, ht, ok := xd.FindWindow("N:application", "ID:com.lab126.KPPMainApp"); ok {
			top, bottom := y, y+ht
			if _, sy, _, sh, ok := xd.FindWindow("N:searchBar", "KPPMainApp"); ok && sy+sh > top {
				top = sy + sh
			}
			if _, by, _, _, ok := xd.FindWindow("N:bottomBar", "KPPMainApp"); ok && by < bottom {
				bottom = by
			}
			if top > 0 && bottom-top > h/3 {
				return top, bottom - top
			}
		}
	}
	top := fallbackTop * w / designWidth
	return top, h - top
}

// fresh computes the stats and draws the card for the Library area at
// (top, libH). It returns the card's position and size; the card image is
// written to cardTmp.
func fresh(scr *screen.Screen, appDir string, top, libH int) (x, y, w, h int, err error) {
	t := time.Now()
	bks := books.Collect(documents, ccdb)
	for _, b := range bks {
		for _, e := range b.Errors {
			warn("sidecar %s: %s", b.Key, e)
		}
	}
	now := clock.Now()
	rows, err := history.Record(snapshots, bks, now, activity.New(activityLog, coversleepLog, deskLog))
	if err != nil {
		warn("reading log not saved: %v", err)
	}
	s := stats.Compute(rows, clock.Date(now), bks)
	info("collected %d books, %d log rows in %.2fs", len(bks), len(rows), time.Since(t).Seconds())

	t = time.Now()
	scale := float64(scr.Width) / designWidth
	fonts, err := card.LoadFonts(card.KindleFonts[0], card.KindleFonts[1], scale)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("fonts: %w", err)
	}
	img := card.Draw(s, clock.Date(now), fonts, scale)
	// too tall for the Library area (e.g. a bottom menu bar): shrink to fit
	need := float64(img.Rect.Dy()) + (cardGap+bottomGap+6)*scale
	if need > float64(libH) {
		scale *= float64(libH) / need
		fonts, _ = card.LoadFonts(card.KindleFonts[0], card.KindleFonts[1], scale)
		img = card.Draw(s, clock.Date(now), fonts, scale)
	}
	framed := card.Frame(img, scale)
	if err := writePNG(cardTmp, framed); err != nil {
		return 0, 0, 0, 0, err
	}
	x = card.Left(scr.Width, scale)
	y = top + int(math.RoundToEven(cardGap*scale))
	info("card drawn at scale %.3f in %.2fs", scale, time.Since(t).Seconds())
	return x, y, framed.Rect.Dx(), framed.Rect.Dy(), nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func run() int {
	setupLogging()
	if !singleInstance() {
		info("already open")
		return 0
	}
	exe, _ := os.Executable()
	appDir := filepath.Dir(filepath.Dir(exe)) // .../inksights/bin/inksights
	if d := os.Getenv("INKSIGHTS_DIR"); d != "" {
		appDir = d
	}
	startRecorder(appDir)
	os.Remove(filepath.Join(dataDir, "card.cache")) // the Python version's cache

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	scr, err := screen.Open()
	if err != nil {
		warn("no screen: %v", err)
		return 1
	}
	defer scr.Close()
	launched := ""
	if v := os.Getenv("RS_LAUNCHED"); v != "" {
		var sec int64
		fmt.Sscan(v, &sec)
		launched = fmt.Sprintf(", %.1fs after tap", time.Since(time.Unix(sec, 0)).Seconds())
	}

	// 1. Last time's card is shown only if it was made from exactly the data
	//    we have now (same fingerprint): then it IS the correct card. Never stale.
	extra := []string{exe}
	saved := loadCache(scr.Width)
	valid := false
	var q string
	if saved != nil {
		t := time.Now()
		q = fingerprint.Quick(documents, snapshots, clock.Date(clock.Now()), extra...)
		valid = q == saved.Quick && fingerprint.Full(q, ccdb) == saved.Full
		state := "data changed, recomputing"
		if valid {
			state = "unchanged, showing saved card"
		}
		info("fingerprint %.2fs: %s", time.Since(t).Seconds(), state)
	}
	var x, y, w, h int
	if valid {
		x, y, w, h = saved.X, saved.Y, saved.W, saved.H
		if err := scr.Show(cachePNG, x, y); err != nil {
			warn("%v", err)
			valid = false
		} else {
			info("card shown %.2fs after start%s", since(), launched)
		}
	}

	// 2. The invisible window over the Library (at last time's position if known).
	top, libH := -1, 0
	if saved != nil {
		top, libH = saved.Top, saved.LibH
	}
	remembered := top >= 0
	var shield uint32
	xd, err := xshield.Open(os.Getenv("DISPLAY"))
	if err != nil {
		warn("no X connection, card may be painted over: %v", err)
		xd = nil
	}
	if top < 0 {
		top, libH = libraryArea(xd, scr.Width, scr.Height)
	}
	if xd != nil {
		if shield, err = xd.Shield(0, top, scr.Width, libH); err != nil {
			warn("no shield, card may be painted over: %v", err)
		} else {
			info("shield at y=%d h=%d (%.2fs)", top, libH, since())
		}
	}
	defer func() {
		if xd != nil {
			if shield != 0 {
				if err := xd.Unmap(shield); err != nil {
					warn("unmap failed: %v", err)
				}
			}
			xd.Close()
		}
	}()

	taps, closeTouch, nTouch := touch.Watch()
	defer closeTouch()
	if nTouch == 0 {
		warn("no touchscreen found; the card closes on sleep or after %v", backstop)
	}
	reasons := make(chan string, 4)
	watchers := watch(reasons)
	defer func() {
		for _, c := range watchers {
			if c.Process != nil {
				c.Process.Kill()
			}
		}
	}()

	if !valid {
		// 3. Fresh stats, drawn over the Library as it is now.
		x, y, w, h, err = fresh(scr, appDir, top, libH)
		if err != nil {
			warn("card failed: %v", err)
			return 1
		}
		if err := scr.Show(cardTmp, x, y); err != nil {
			warn("%v", err)
			return 1
		}
		info("card shown %.2fs after start%s", since(), launched)

		// 4. Remembered Library position still right? (not time-critical now)
		saveTop, saveH := top, libH
		if remembered && xd != nil {
			if rt, rh := libraryArea(xd, scr.Width, scr.Height); rt != top || rh != libH {
				warn("Library moved to y=%d h=%d; next launch will use it", rt, rh)
				saveTop, saveH = rt, rh
			}
		}
		// fingerprint AFTER computing: logging this launch's reading changes the inputs
		q = fingerprint.Quick(documents, snapshots, clock.Date(clock.Now()), extra...)
		saveCache(cache{ScreenW: scr.Width, X: x, Y: y, W: w, H: h, Top: saveTop, LibH: saveH,
			Quick: q, Full: fingerprint.Full(q, ccdb)})

		// 5. Reading data changed, so refresh the sidecar backups (card's already up).
		t := time.Now()
		n := backup.MirrorSidecars(documents, sidecarBak)
		info("backed up %d sidecar file(s) in %.2fs", n, time.Since(t).Seconds())
	}

	drawn := scr.Grab(x, y, w, h)
	shown := cachePNG
	if !valid {
		shown = cardTmp
	}
	check := time.NewTimer(recheck)
	end := time.NewTimer(backstop - time.Since(tStart))
	for {
		select {
		case <-check.C:
			// only redraw if something actually painted over the card
			if drawn != nil && string(scr.Grab(x, y, w, h)) != string(drawn) {
				scr.Show(shown, x, y)
				info("card was painted over; redrawn")
			}
		case <-end.C:
			info("closed: backstop timeout")
			return 0
		case r := <-reasons:
			info("closed: %s", r)
			return 0
		case t := <-taps:
			where := "card/library"
			if t.Y >= 0 && t.Y < top {
				where = "menu bar"
			}
			info("closed: tap at (%d, %d) (%s)", t.X, t.Y, where)
			return 0
		case s := <-sigs:
			info("closed: %v", s)
			return 0
		}
	}
}
