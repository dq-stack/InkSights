// Package screen shows images on the Kindle's e-ink screen.
//
// Drawing and refreshing go through an FBInk binary that's already on the
// Kindle (installed by the jailbreak or by another app), because each
// Kindle's e-ink controller needs different refresh calls and FBInk knows
// them all. Reading back what's on screen is done here directly from the
// framebuffer, which is the same everywhere (8-bit grey on most Kindles).
package screen

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FBInkCandidates are places an FBInk binary may be, best first. The
// hard-float or soft-float folder is picked to match this Kindle.
func FBInkCandidates() []string {
	arch := "kindlepw2"
	if _, err := os.Stat("/lib/ld-linux-armhf.so.3"); err == nil {
		arch = "kindlehf"
	}
	return []string{
		"/var/local/kmc/" + arch + "/bin/fbink", // modern jailbreak (hdnext)
		"/mnt/us/libkh/bin/fbink",
		"/mnt/us/extensions/custom-screensaver/bin/fbink_hf",
		"/mnt/us/extensions/custom-screensaver/bin/fbink",
		"/mnt/us/extensions/MRInstaller/bin/fbink",
		"/mnt/us/usbnet/bin/fbink",
		"/usr/bin/fbink",
		"/mnt/us/extensions/inksights/bin/fbink",
	}
}

// FindFBInk returns the first FBInk binary that runs on this Kindle.
func FindFBInk() (string, error) {
	for _, p := range FBInkCandidates() {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		// a soft-float binary on a hard-float Kindle (or the other way) can't
		// even start; one that starts and exits (whatever its status) works
		err = exec.Command(p, "-h").Run()
		var exit *exec.ExitError
		if err == nil || errors.As(err, &exit) {
			return p, nil
		}
	}
	return "", errors.New("no working FBInk found (install one with your jailbreak's tools)")
}

// Screen is the framebuffer plus the FBInk binary used to draw on it.
type Screen struct {
	FBInk  string
	Width  int
	Height int
	fb     *framebuffer
}

// Open finds FBInk and the framebuffer's size.
func Open() (*Screen, error) {
	fbink, err := FindFBInk()
	if err != nil {
		return nil, err
	}
	fb, err := openFramebuffer("/dev/fb0")
	if err != nil {
		return nil, err
	}
	return &Screen{FBInk: fbink, Width: fb.width, Height: fb.height, fb: fb}, nil
}

// Deferred is a screen of a known size that doesn't touch the display (no
// FBInk run, no framebuffer opened) until the first draw. On 5.18 any
// display activity during a scriptlet launch postpones the Kindle's launch
// flash, so the card is prepared without touching the screen.
func Deferred(width, height int) *Screen {
	return &Screen{Width: width, Height: height}
}

// Show draws an image file (PNG; transparent parts blend over the screen)
// with its top-left corner at (x, y) and refreshes that area without the
// black flash (GL16, what the Kindle uses for ordinary page turns).
func (s *Screen) Show(path string, x, y int) error {
	if s.FBInk == "" {
		p, err := FindFBInk()
		if err != nil {
			return err
		}
		s.FBInk = p
	}
	args := []string{"-q", "-W", "GL16", "-g", fmt.Sprintf("file=%s,x=%d,y=%d", path, x, y)}
	out, err := exec.Command(s.FBInk, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("fbink: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Grab returns the screen contents of a rectangle as w*h grey bytes, or
// nil if the framebuffer can't be read that way.
func (s *Screen) Grab(x, y, w, h int) []byte {
	if s.fb == nil {
		return nil
	}
	return s.fb.grab(x, y, w, h)
}

// Close releases the framebuffer.
func (s *Screen) Close() {
	if s.fb != nil {
		s.fb.close()
	}
}
