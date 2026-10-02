"""Passive touchscreen reader: tells us when a tap ends and where.

Opens the touch device read-only and never grabs it, so the Kindle UI still
gets every tap (that's what keeps the menu bar working while the card is up).
"""

import glob
import os
import struct

EV_KEY, EV_ABS = 1, 3
BTN_TOUCH = 0x14a
ABS_X, ABS_Y = 0x00, 0x01
ABS_MT_POSITION_X, ABS_MT_POSITION_Y = 0x35, 0x36

EVENT = struct.Struct("llHHi")   # struct input_event (native long timeval)


def touch_devices(text=None):
    """event device paths whose capabilities include BTN_TOUCH."""
    paths = []
    if text is None:
        try:
            with open("/proc/bus/input/devices") as f:
                text = f.read()
        except OSError:
            return sorted(glob.glob("/dev/input/event*"))
    for block in text.split("\n\n"):
        handlers = [w for line in block.splitlines() if line.startswith("H: Handlers=")
                    for w in line[len("H: Handlers="):].split() if w.startswith("event")]
        keys = [line.split("=", 1)[1].split() for line in block.splitlines() if line.startswith("B: KEY=")]
        if not handlers or not keys:
            continue
        # KEY bitmap is printed as hex words, most significant first
        words = keys[0]
        bit = BTN_TOUCH
        word_bits = 64 if any(len(w) > 8 for w in words) else 32
        idx = len(words) - 1 - bit // word_bits
        if 0 <= idx < len(words) and int(words[idx], 16) >> (bit % word_bits) & 1:
            paths.append("/dev/input/" + handlers[0])
    return paths


class Touch(object):
    def __init__(self):
        self.fds = []
        self.paths = []
        for p in touch_devices():
            try:
                self.fds.append(os.open(p, os.O_RDONLY | os.O_NONBLOCK))
                self.paths.append(p)
            except OSError:
                pass
        self.x = self.y = None
        self._buf = {}

    def read(self, fd):
        """Consume pending events on fd; return (x, y) of a tap that just
        ended, else None."""
        try:
            data = os.read(fd, EVENT.size * 64)
        except (BlockingIOError, OSError):
            return None
        data = self._buf.pop(fd, b"") + data
        tap = None
        n = len(data) // EVENT.size
        for i in range(n):
            _s, _us, typ, code, val = EVENT.unpack_from(data, i * EVENT.size)
            if typ == EV_ABS and code in (ABS_MT_POSITION_X, ABS_X):
                self.x = val
            elif typ == EV_ABS and code in (ABS_MT_POSITION_Y, ABS_Y):
                self.y = val
            elif typ == EV_KEY and code == BTN_TOUCH and val == 0:
                tap = (self.x, self.y)
        rest = data[n * EVENT.size:]
        if rest:
            self._buf[fd] = rest
        return tap

    def close(self):
        for fd in self.fds:
            try:
                os.close(fd)
            except OSError:
                pass
