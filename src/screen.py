"""Framebuffer access through FBInk's Python bindings (bundled with the
Kindle Python package).

Works on raw 8-bit grey bytes only, so the fast path (showing the cached
card) never has to import Pillow.
"""

from _fbink import ffi, lib


class Screen(object):
    def __init__(self):
        self.fd = lib.fbink_open()
        if self.fd < 0:
            raise RuntimeError("fbink_open failed")
        self.cfg = ffi.new("FBInkConfig *")
        self.cfg.is_quiet = True
        if lib.fbink_init(self.fd, self.cfg) < 0:
            raise RuntimeError("fbink_init failed")
        state = ffi.new("FBInkState *")
        lib.fbink_get_state(self.cfg, state)
        self.width = state.view_width
        self.height = state.view_height

    def grab(self, x, y, w, h):
        """Screen contents of that rectangle as w*h grey bytes (white where
        the framebuffer can't be read or is in a format we don't handle)."""
        white = b"\xff" * (w * h)
        dump = ffi.new("FBInkDump *")
        try:
            if lib.fbink_region_dump(self.fd, x, y, w, h, self.cfg, dump) < 0:
                return white
            aw, ah, stride = dump.area.width, dump.area.height, dump.stride
            if not aw or stride // aw != 1:      # only 8bpp (PW2 and most Kindles)
                return white
            raw = ffi.buffer(dump.data, stride * ah)
            rows = []
            for r in range(h):
                if r < ah:
                    row = bytes(raw[r * stride:r * stride + min(aw, w)])
                    rows.append(row + b"\xff" * (w - len(row)))
                else:
                    rows.append(b"\xff" * w)
            return b"".join(rows)
        finally:
            lib.fbink_free_dump_data(dump)

    def show(self, data, w, h, x, y, flash=False):
        """Draw w*h grey bytes at (x, y) and refresh that region.

        Default is GL16: greyscale without the black flash (what the Kindle
        uses for ordinary page turns). flash=True forces a full flashing
        refresh, which is crisper but blinks."""
        self.cfg.is_flashing = bool(flash)
        self.cfg.wfm_mode = lib.WFM_GC16 if flash else lib.WFM_GL16
        rc = lib.fbink_print_raw_data(self.fd, data, w, h, len(data), x, y, self.cfg)
        self.cfg.is_flashing = False
        self.cfg.wfm_mode = lib.WFM_AUTO
        if rc < 0:
            raise RuntimeError("fbink_print_raw_data failed (%d)" % rc)

    def close(self):
        lib.fbink_close(self.fd)
