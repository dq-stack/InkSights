//go:build linux

package screen

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602
)

type framebuffer struct {
	f               *os.File
	width, height   int
	xoff, yoff      int
	bpp, lineLength int
}

func ioctl(fd uintptr, req uintptr, buf []byte) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(&buf[0])))
	if e != 0 {
		return e
	}
	return nil
}

func openFramebuffer(path string) (*framebuffer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// struct fb_var_screeninfo: 40 u32 fields (160 bytes); xres, yres,
	// xres_virtual, yres_virtual, xoffset, yoffset, bits_per_pixel, ...
	v := make([]byte, 160)
	if err := ioctl(f.Fd(), fbioGetVScreenInfo, v); err != nil {
		f.Close()
		return nil, err
	}
	u := func(b []byte, i int) int { return int(binary.LittleEndian.Uint32(b[i*4:])) }
	fb := &framebuffer{f: f, width: u(v, 0), height: u(v, 1), xoff: u(v, 4), yoff: u(v, 5), bpp: u(v, 6)}
	// struct fb_fix_screeninfo: line_length sits after id[16], smem_start
	// (a pointer-sized unsigned long), smem_len, type, type_aux, visual and
	// three u16 pan/wrap steps (+2 bytes padding)
	fx := make([]byte, 128)
	if err := ioctl(f.Fd(), fbioGetFScreenInfo, fx); err != nil {
		f.Close()
		return nil, err
	}
	off := 16 + int(unsafe.Sizeof(uintptr(0))) + 4*4 + 2*3 + 2
	fb.lineLength = int(binary.LittleEndian.Uint32(fx[off:]))
	if fb.width <= 0 || fb.height <= 0 || fb.lineLength <= 0 {
		f.Close()
		return nil, errors.New("framebuffer reports no size")
	}
	return fb, nil
}

func (fb *framebuffer) grab(x, y, w, h int) []byte {
	if fb.bpp != 8 || w <= 0 || h <= 0 {
		return nil // only 8-bit grey (most Kindles)
	}
	out := make([]byte, w*h)
	for r := 0; r < h; r++ {
		off := int64((fb.yoff+y+r)*fb.lineLength + fb.xoff + x)
		if _, err := fb.f.ReadAt(out[r*w:(r+1)*w], off); err != nil {
			return nil
		}
	}
	return out
}

func (fb *framebuffer) close() { fb.f.Close() }
