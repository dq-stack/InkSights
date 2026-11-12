// Package touch watches the touchscreen for taps without taking it over.
//
// The device is opened read-only and never grabbed, so the Kindle UI still
// gets every tap (that's what keeps the menu bar working while the card is
// up).
package touch

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"
)

const (
	evKey          = 1
	evAbs          = 3
	btnTouch       = 0x14a
	absX, absY     = 0x00, 0x01
	absMTX, absMTY = 0x35, 0x36
)

// Devices returns the event devices whose key capabilities include
// BTN_TOUCH, from /proc/bus/input/devices text.
func Devices(text string) []string {
	var paths []string
	for _, block := range strings.Split(text, "\n\n") {
		var handler string
		var keys []string
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "H: Handlers="):
				for _, w := range strings.Fields(line[len("H: Handlers="):]) {
					if strings.HasPrefix(w, "event") && handler == "" {
						handler = w
					}
				}
			case strings.HasPrefix(line, "B: KEY="):
				keys = strings.Fields(line[len("B: KEY="):])
			}
		}
		if handler == "" || len(keys) == 0 {
			continue
		}
		// the KEY bitmap is printed as hex words, most significant first
		wordBits := 32
		for _, w := range keys {
			if len(w) > 8 {
				wordBits = 64
			}
		}
		idx := len(keys) - 1 - btnTouch/wordBits
		if idx < 0 || idx >= len(keys) {
			continue
		}
		v, err := strconv.ParseUint(keys[idx], 16, 64)
		if err == nil && v>>(btnTouch%wordBits)&1 == 1 {
			paths = append(paths, "/dev/input/"+handler)
		}
	}
	return paths
}

// Tap is where a touch ended (screen coordinates on most Kindles; -1 if unknown).
type Tap struct{ X, Y int }

// Watch opens every touch device and sends a Tap each time a touch ends.
// The returned function closes the devices.
func Watch() (<-chan Tap, func(), int) {
	taps := make(chan Tap, 4)
	text, err := os.ReadFile("/proc/bus/input/devices")
	var paths []string
	if err == nil {
		paths = Devices(string(text))
	} else {
		paths, _ = filepath.Glob("/dev/input/event*")
	}
	var files []*os.File
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		files = append(files, f)
		go read(f, taps)
	}
	return taps, func() {
		for _, f := range files {
			f.Close()
		}
	}, len(files)
}

// struct input_event: a timeval (two native longs) then u16 type, u16
// code, s32 value.
var eventSize = 2*int(unsafe.Sizeof(uintptr(0))) + 8

func read(f *os.File, taps chan<- Tap) {
	x, y := -1, -1
	buf := make([]byte, eventSize*64)
	var rest []byte
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		data := append(rest, buf[:n]...)
		for len(data) >= eventSize {
			ev := data[eventSize-8 : eventSize]
			typ := binary.LittleEndian.Uint16(ev[0:])
			code := binary.LittleEndian.Uint16(ev[2:])
			val := int(int32(binary.LittleEndian.Uint32(ev[4:])))
			switch {
			case typ == evAbs && (code == absMTX || code == absX):
				x = val
			case typ == evAbs && (code == absMTY || code == absY):
				y = val
			case typ == evKey && code == btnTouch && val == 0:
				select {
				case taps <- Tap{x, y}:
				default:
				}
			}
			data = data[eventSize:]
		}
		rest = append([]byte(nil), data...)
	}
}
