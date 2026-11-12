//go:build !linux

package screen

import "errors"

type framebuffer struct{ width, height int }

func openFramebuffer(string) (*framebuffer, error) {
	return nil, errors.New("framebuffer access needs a Kindle")
}

func (fb *framebuffer) grab(x, y, w, h int) []byte { return nil }
func (fb *framebuffer) close()                     {}
