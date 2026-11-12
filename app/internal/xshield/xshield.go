// Package xshield is a minimal X11 client: an invisible, input-catching
// window over part of the screen.
//
// While it's mapped, the Kindle UI underneath can't paint over the card
// drawn into the framebuffer, and taps on it go to us instead of the
// Library. Unmapping it (or just closing the connection) makes the UI
// repaint itself. It speaks the X11 wire protocol directly, so no Xlib.
package xshield

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

const (
	xCreateWindow        = 1
	xGetWindowAttributes = 3
	xMapWindow           = 8
	xUnmapWindow         = 10
	xConfigureWindow     = 12
	xGetGeometry         = 14
	xQueryTree           = 15
	xGetProperty         = 20
	xGetInputFocus       = 43

	cwBackPixmap       = 0x0001
	cwOverrideRedirect = 0x0200
	atomWMName         = 39
)

var le = binary.LittleEndian

func pad(n int) int { return (4 - n%4) % 4 }

// Display is a connection to the X server.
type Display struct {
	conn           net.Conn
	seq            uint16
	idBase, idMask uint32
	nextID         uint32
	Root           uint32
	Width, Height  int
}

// Open connects to a display such as ":0".
func Open(display string) (*Display, error) {
	num := "0"
	if i := strings.Index(display, ":"); i >= 0 {
		num = strings.SplitN(display[i+1:], ".", 2)[0]
	}
	c, err := net.DialTimeout("unix", "/tmp/.X11-unix/X"+num, 5*time.Second)
	if err != nil {
		return nil, err
	}
	d := &Display{conn: c, nextID: 1}
	name, data := auth(num)
	if err := d.setup(name, data); err != nil {
		c.Close()
		return nil, err
	}
	return d, nil
}

// auth returns a MIT-MAGIC-COOKIE-1 from $XAUTHORITY if there is one
// (the Kindle's X server normally runs without auth).
func auth(num string) ([]byte, []byte) {
	path := os.Getenv("XAUTHORITY")
	data, err := os.ReadFile(path)
	if path == "" || err != nil {
		return nil, nil
	}
	for i := 0; i+2 <= len(data); {
		i += 2 // family
		var fields [4][]byte
		for f := 0; f < 4; f++ {
			if i+2 > len(data) {
				return nil, nil
			}
			n := int(binary.BigEndian.Uint16(data[i:]))
			if i+2+n > len(data) {
				return nil, nil
			}
			fields[f] = data[i+2 : i+2+n]
			i += 2 + n
		}
		dnum := string(fields[1])
		if (dnum == num || dnum == "") && string(fields[2]) == "MIT-MAGIC-COOKIE-1" {
			return fields[2], fields[3]
		}
	}
	return nil, nil
}

func (d *Display) read(n int) ([]byte, error) {
	buf := make([]byte, n)
	d.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(d.conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *Display) setup(authName, authData []byte) error {
	var req bytes.Buffer
	req.WriteByte('l')
	req.WriteByte(0)
	binary.Write(&req, le, [3]uint16{11, 0, uint16(len(authName))})
	binary.Write(&req, le, uint16(len(authData)))
	req.Write([]byte{0, 0})
	req.Write(authName)
	req.Write(make([]byte, pad(len(authName))))
	req.Write(authData)
	req.Write(make([]byte, pad(len(authData))))
	if _, err := d.conn.Write(req.Bytes()); err != nil {
		return err
	}
	head, err := d.read(8)
	if err != nil {
		return err
	}
	extra := int(le.Uint16(head[6:]))
	body, err := d.read(extra * 4)
	if err != nil {
		return err
	}
	if head[0] != 1 {
		n := int(head[1])
		if n > len(body) {
			n = len(body)
		}
		return fmt.Errorf("X connection refused: %q", body[:n])
	}
	d.idBase, d.idMask = le.Uint32(body[4:]), le.Uint32(body[8:])
	vendorLen := int(le.Uint16(body[16:]))
	nFormats := int(body[21])
	off := 32 + vendorLen + pad(vendorLen) + 8*nFormats
	if off+24 > len(body) {
		return errors.New("short X setup reply")
	}
	d.Root = le.Uint32(body[off:])
	d.Width, d.Height = int(le.Uint16(body[off+20:])), int(le.Uint16(body[off+22:]))
	return nil
}

func (d *Display) send(data []byte) uint16 {
	d.seq++
	d.conn.Write(data)
	return d.seq
}

// reply reads until the reply for seq (errors for it are returned; events
// and other replies are skipped).
func (d *Display) reply(seq uint16) ([]byte, error) {
	for {
		pkt, err := d.read(32)
		if err != nil {
			return nil, err
		}
		pseq := le.Uint16(pkt[2:])
		switch pkt[0] {
		case 0:
			if pseq == seq {
				return nil, fmt.Errorf("X error %d for request %d", pkt[1], seq)
			}
		case 1:
			extra := int(le.Uint32(pkt[4:]))
			if extra > 0 {
				more, err := d.read(extra * 4)
				if err != nil {
					return nil, err
				}
				pkt = append(pkt, more...)
			}
			if pseq == seq {
				return pkt, nil
			}
		}
	}
}

func req(op byte, data byte, words ...uint32) []byte {
	b := make([]byte, 4+4*len(words))
	b[0], b[1] = op, data
	le.PutUint16(b[2:], uint16(1+len(words)))
	for i, w := range words {
		le.PutUint32(b[4+4*i:], w)
	}
	return b
}

func (d *Display) sync() error {
	_, err := d.reply(d.send(req(xGetInputFocus, 0)))
	return err
}

// Children lists a window's children, bottom to top.
func (d *Display) Children(w uint32) ([]uint32, error) {
	r, err := d.reply(d.send(req(xQueryTree, 0, w)))
	if err != nil {
		return nil, err
	}
	n := int(le.Uint16(r[16:]))
	out := make([]uint32, 0, n)
	for i := 0; i < n && 32+4*i+4 <= len(r); i++ {
		out = append(out, le.Uint32(r[32+4*i:]))
	}
	return out, nil
}

// Name is a window's WM_NAME.
func (d *Display) Name(w uint32) (string, error) {
	r, err := d.reply(d.send(req(xGetProperty, 0, w, atomWMName, 0, 0, 256)))
	if err != nil {
		return "", err
	}
	n := int(le.Uint32(r[16:]))
	if 32+n > len(r) {
		n = len(r) - 32
	}
	return string(r[32 : 32+n]), nil
}

// Viewable reports whether a window is mapped and visible.
func (d *Display) Viewable(w uint32) (bool, error) {
	r, err := d.reply(d.send(req(xGetWindowAttributes, 0, w)))
	if err != nil {
		return false, err
	}
	return r[26] == 2, nil
}

// Geometry is a window's position and size.
func (d *Display) Geometry(w uint32) (x, y, wd, ht int, err error) {
	r, err := d.reply(d.send(req(xGetGeometry, 0, w)))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return int(int16(le.Uint16(r[12:]))), int(int16(le.Uint16(r[14:]))),
		int(le.Uint16(r[16:])), int(le.Uint16(r[18:])), nil
}

// FindWindow returns the geometry of the first viewable top-level window
// whose name contains every needle.
func (d *Display) FindWindow(needles ...string) (x, y, w, h int, ok bool) {
	kids, err := d.Children(d.Root)
	if err != nil {
		return
	}
	for _, k := range kids {
		name, err := d.Name(k)
		if err != nil || !containsAll(name, needles) {
			continue
		}
		if v, err := d.Viewable(k); err != nil || !v {
			continue
		}
		if x, y, w, h, err := d.Geometry(k); err == nil {
			return x, y, w, h, true
		}
	}
	return
}

func containsAll(s string, subs []string) bool {
	for _, n := range subs {
		if !strings.Contains(s, n) {
			return false
		}
	}
	return len(subs) > 0
}

// Shield maps an invisible input-catching window and returns its id.
func (d *Display) Shield(x, y, w, h int) (uint32, error) {
	wid := d.idBase | (d.nextID & d.idMask)
	d.nextID++
	b := make([]byte, 40)
	b[0] = xCreateWindow
	b[1] = 0 // depth: copy from parent
	le.PutUint16(b[2:], 10)
	le.PutUint32(b[4:], wid)
	le.PutUint32(b[8:], d.Root)
	le.PutUint16(b[12:], uint16(int16(x)))
	le.PutUint16(b[14:], uint16(int16(y)))
	le.PutUint16(b[16:], uint16(w))
	le.PutUint16(b[18:], uint16(h))
	le.PutUint16(b[20:], 0) // border
	le.PutUint16(b[22:], 1) // InputOutput
	le.PutUint32(b[24:], 0) // visual: copy from parent
	le.PutUint32(b[28:], cwBackPixmap|cwOverrideRedirect)
	le.PutUint32(b[32:], 0) // background None (see-through)
	le.PutUint32(b[36:], 1) // override-redirect
	d.send(b)
	d.send(req(xMapWindow, 0, wid))
	// stack-mode Above, in case something else is override-redirect too
	cfg := make([]byte, 16)
	cfg[0] = xConfigureWindow
	le.PutUint16(cfg[2:], 4)
	le.PutUint32(cfg[4:], wid)
	le.PutUint16(cfg[8:], 0x40)
	le.PutUint32(cfg[12:], 0)
	d.send(cfg)
	return wid, d.sync()
}

// Unmap hides the shield, letting the Kindle UI repaint.
func (d *Display) Unmap(wid uint32) error {
	d.send(req(xUnmapWindow, 0, wid))
	return d.sync()
}

// Close disconnects (the server then destroys our window).
func (d *Display) Close() { d.conn.Close() }
