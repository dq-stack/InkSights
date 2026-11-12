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
	cacheVer    = 2
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
	Focus   bool   `json:"focus"`
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

// libraryArea returns where the card goes: (top, height) of the screen
// area to cover, and whether to blank that area ("focus").
//
// Firmware up to 5.15 or so: the Library's content below its menu bar, which
// stays usable, and the card is drawn over the Library. Newer firmware
// (seen on 5.18): the Library app (KPPMainApp) repaints its search bar and
// bottom bar over anything drawn there, so everything below the status bar
// is covered and blanked, with the card in the middle.
func libraryArea(xd *xshield.Display, w, h int) (int, int, bool) {
	if holding {
		return 0, h, true // the Library is paused: the whole screen is ours
	}
	if xd != nil {
		// Match the application window, not the Library's "⋮" menu dialog,
		// which shares the booklet id.
		if _, y, _, ht, ok := xd.FindWindow("N:application_ID:com.lab126.booklet.home"); ok && y > 0 {
			return y, ht, false
		}
		if _, y, _, _, ok := xd.FindWindow("N:application", "ID:com.lab126.KPPMainApp"); ok {
			top := y
			if _, ty, _, th, ok := xd.FindWindow("N:titleBar"); ok && ty+th > 0 && ty+th < h/4 {
				top = ty + th
			}
			if top >= 0 && top < h/4 {
				return top, h - top, true
			}
		}
		// not found: note what's on screen, to support other firmware
		for _, l := range xd.ViewableList() {
			info("window: %s", l)
		}
	}
	top := fallbackTop * w / designWidth
	warn("Library window not found; using y=%d", top)
	return top, h - top, false
}

// fresh computes the stats and draws the card for the area at (top, areaH).
// It returns where the image written to cardTmp goes and its size: just the
// card (transparent corners, blended over the Library), or with focus the
// whole area, blank with the card in the middle.
func fresh(scr *screen.Screen, top, areaH int, focus bool) (x, y, w, h int, err error) {
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
	// too tall for the area: shrink to fit
	need := float64(img.Rect.Dy()) + (cardGap+bottomGap+6)*scale
	if need > float64(areaH) {
		scale *= float64(areaH) / need
		fonts, _ = card.LoadFonts(card.KindleFonts[0], card.KindleFonts[1], scale)
		img = card.Draw(s, clock.Date(now), fonts, scale)
	}
	framed := card.Frame(img, scale)
	var out image.Image = framed
	if focus {
		x, y, w, h = 0, top, scr.Width, areaH
		out = card.OnBlank(framed, w, h)
	} else {
		x = card.Left(scr.Width, scale)
		y = top + int(math.RoundToEven(cardGap*scale))
		w, h = framed.Rect.Dx(), framed.Rect.Dy()
	}
	if err := writePNG(cardTmp, out); err != nil {
		return 0, 0, 0, 0, err
	}
	info("card drawn at scale %.3f in %.2fs", scale, time.Since(t).Seconds())
	return x, y, w, h, nil
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

// showX shows an image file in an X window that also catches taps (used
// while holding the Library; see ImageWindow).
func showX(xd *xshield.Display, shield *uint32, path string, x, y, w, h int) error {
	if xd == nil {
		return fmt.Errorf("no X connection")
	}
	img := loadPNG(path)
	if img == nil {
		return fmt.Errorf("can't read %s", path)
	}
	b := img.Bounds()
	grey := make([]byte, w*h)
	for i := range grey {
		grey[i] = 0xFF
	}
	for yy := 0; yy < h && yy < b.Dy(); yy++ {
		for xx := 0; xx < w && xx < b.Dx(); xx++ {
			r, _, _, a := img.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
			if a == 0 {
				continue
			}
			grey[yy*w+xx] = uint8(r >> 8)
		}
	}
	wid, err := xd.ImageWindow(x, y, w, h, grey)
	if err != nil {
		return err
	}
	*shield = wid
	return nil
}

func loadPNG(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

// onScreen reports whether the screen grab still shows the card: its solid
// pixels match within a little tolerance (the e-ink driver may round greys).
func onScreen(grab []byte, want image.Image) bool {
	if grab == nil {
		return true // can't tell: leave it
	}
	b := want.Bounds()
	w := b.Dx()
	if len(grab) < w*b.Dy() {
		return true
	}
	bad, solid := 0, 0
	for y := 0; y < b.Dy(); y += 2 {
		for x := 0; x < w; x += 2 {
			r, _, _, a := want.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a != 0xffff {
				continue
			}
			solid++
			d := int(grab[y*w+x]) - int(r>>8)
			if d < -24 || d > 24 {
				bad++
			}
		}
	}
	return solid == 0 || bad*100 < solid // under 1% off
}

// holding is set when InkSights runs in the foreground of the scriptlet
// launcher, keeping the Library paused while the card is up (see run).
var holding bool

// launcherHoldsLibrary reports whether we were started by the scriptlet
// launcher on firmware whose Library is KPPMainApp (5.18): there the
// launcher pauses the Library until the script ends, and the Library
// flashes the screen when it comes back.
func launcherHoldsLibrary() bool {
	out, err := exec.Command("lipc-get-prop", "com.lab126.appmgrd", "activeApp").Output()
	if err != nil || !strings.Contains(string(out), "shell_integration.launcher") {
		return false
	}
	xd, err := xshield.Open(os.Getenv("DISPLAY"))
	if err != nil {
		return false
	}
	defer xd.Close()
	holdW, holdH = xd.Width, xd.Height
	return xd.Exists("ID:com.lab126.KPPMainApp") && holdW > 0 && holdH > 0
}

// holdW, holdH are the screen size from X, used while holding so the
// display isn't touched before the card is drawn.
var holdW, holdH int

// detach starts a copy of this program in its own session and returns, so
// the launcher script can end and the Library carries on behind the card.
func detach() int {
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "INKSIGHTS_FOREGROUND=") {
			env = append(env, e)
		}
	}
	cmd := exec.Command(exe)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		warn("couldn't detach: %v", err)
		return 1
	}
	cmd.Process.Release()
	return 0
}

func run() int {
	setupLogging()
	// The launcher script runs us in the foreground. On 5.18 we stay there:
	// while we run, the launcher keeps the Library paused, so it can't wake
	// up and flash the screen while the card is up; it comes back (with its
	// flash) as the card closes. Elsewhere, detach so the Library stays live
	// behind the card.
	if os.Getenv("INKSIGHTS_FOREGROUND") == "1" {
		if holding = launcherHoldsLibrary(); !holding {
			return detach()
		}
	}
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

	var scr *screen.Screen
	var err error
	if holding {
		scr = screen.Deferred(holdW, holdH)
	} else if scr, err = screen.Open(); err != nil {
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
	if saved != nil && holding != (saved.Top == 0 && saved.LibH == scr.Height && saved.Focus) {
		saved = nil // made for the other way of showing it
	}
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
	}
	if valid && !holding {
		if err := scr.Show(cachePNG, x, y); err != nil {
			warn("%v", err)
			valid = false
		} else {
			info("card shown %.2fs after start%s", since(), launched)
		}
	}

	// 2. The invisible window over the Library (at last time's position if known).
	top, libH, focus := -1, 0, false
	if saved != nil {
		top, libH, focus = saved.Top, saved.LibH, saved.Focus
	}
	remembered := top >= 0
	var shield uint32
	xd, err := xshield.Open(os.Getenv("DISPLAY"))
	if err != nil {
		warn("no X connection, card may be painted over: %v", err)
		xd = nil
	}
	if top < 0 {
		top, libH, focus = libraryArea(xd, scr.Width, scr.Height)
	}
	// No separate shield while holding: the card itself is then an X window
	// (see showX), which catches taps, over a Library that's paused.
	if xd != nil && !holding {
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

	if valid && holding {
		err := showX(xd, &shield, cachePNG, x, y, w, h)
		if err != nil {
			warn("X drawing failed (%v); using FBInk", err)
			err = scr.Show(cachePNG, x, y)
		}
		if err != nil {
			warn("%v", err)
			valid = false
		} else {
			info("card shown %.2fs after start%s", since(), launched)
		}
	}

	if !valid {
		// 3. Fresh stats, drawn over the Library as it is now.
		x, y, w, h, err = fresh(scr, top, libH, focus)
		if err != nil {
			warn("card failed: %v", err)
			return 1
		}
		show := scr.Show
		if holding {
			show = func(p string, x, y int) error {
				if err := showX(xd, &shield, p, x, y, w, h); err != nil {
					warn("X drawing failed (%v); using FBInk", err)
					return scr.Show(p, x, y)
				}
				return nil
			}
		}
		if err := show(cardTmp, x, y); err != nil {
			warn("%v", err)
			return 1
		}
		info("card shown %.2fs after start%s", since(), launched)

		// 4. Remembered Library position still right? (not time-critical now)
		saveTop, saveH, saveFocus := top, libH, focus
		if remembered && xd != nil {
			if rt, rh, rf := libraryArea(xd, scr.Width, scr.Height); rt != top || rh != libH || rf != focus {
				warn("Library moved to y=%d h=%d; next launch will use it", rt, rh)
				saveTop, saveH, saveFocus = rt, rh, rf
			}
		}
		// fingerprint AFTER computing: logging this launch's reading changes the inputs
		q = fingerprint.Quick(documents, snapshots, clock.Date(clock.Now()), extra...)
		saveCache(cache{ScreenW: scr.Width, X: x, Y: y, W: w, H: h, Top: saveTop, LibH: saveH, Focus: saveFocus,
			Quick: q, Full: fingerprint.Full(q, ccdb)})

		// 5. Reading data changed, so refresh the sidecar backups (card's already up).
		t := time.Now()
		n := backup.MirrorSidecars(documents, sidecarBak)
		info("backed up %d sidecar file(s) in %.2fs", n, time.Since(t).Seconds())
	}

	shown := cachePNG
	if !valid {
		shown = cardTmp
	}
	want := loadPNG(shown)
	// The Library may repaint itself in the first seconds after a launch
	// (more than once on newer firmware), so compare the screen with the card
	// a few times and redraw whatever got painted over.
	shownAt := time.Now()
	checks := []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, recheck, 6 * time.Second}
	if holding {
		// The card is an X window over a paused Library: nothing can paint over it.
		checks = []time.Duration{backstop}
	}
	check := time.NewTimer(checks[0])
	end := time.NewTimer(backstop - time.Since(tStart))
	for {
		select {
		case <-check.C:
			if want != nil && !onScreen(scr.Grab(x, y, w, h), want) {
				scr.Show(shown, x, y)
				info("card was painted over; redrawn (%.1fs)", since())
			}
			checks = checks[1:]
			if len(checks) > 0 {
				check.Reset(checks[0] - time.Since(shownAt))
			}
		case <-end.C:
			info("closed: backstop timeout")
			return 0
		case r := <-reasons:
			info("closed: %s", r)
			return 0
		case t := <-taps:
			where := "card/library"
			if !focus && t.Y >= 0 && t.Y < top {
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
