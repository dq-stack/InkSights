"""Minimal raw-X11 client: an invisible, input-catching window over part of
the screen.

While it is mapped, the Kindle UI underneath can't paint over the card we
draw into the framebuffer, and taps on it go to us instead of the Library.
Unmapping it (or just closing the connection) makes the UI repaint itself.

Same idea as CoverSleep's screensaver_shield (C/Xlib), but speaking the X11
wire protocol directly so the app needs no compiled helper.
"""

import os
import socket
import struct

X_CREATE_WINDOW = 1
X_GET_WINDOW_ATTRIBUTES = 3
X_MAP_WINDOW = 8
X_UNMAP_WINDOW = 10
X_CONFIGURE_WINDOW = 12
X_GET_GEOMETRY = 14
X_QUERY_TREE = 15
X_GET_PROPERTY = 20
X_GET_INPUT_FOCUS = 43

CW_BACK_PIXMAP = 0x0001
CW_OVERRIDE_REDIRECT = 0x0200
CW_EVENT_MASK = 0x0800
ATOM_WM_NAME = 39


class XError(Exception):
    pass


def _pad(n):
    return (4 - n % 4) % 4


class Display(object):
    def __init__(self, display=":0"):
        num = display.split(":")[1].split(".")[0] if ":" in display else "0"
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(5)
        self.sock.connect("/tmp/.X11-unix/X%s" % num)
        self.seq = 0
        self._setup(*self._auth(num))

    def _auth(self, num):
        """MIT-MAGIC-COOKIE-1 from $XAUTHORITY if there is one, else none
        (the Kindle's X server normally runs without auth)."""
        path = os.environ.get("XAUTHORITY")
        if not path or not os.path.exists(path):
            return b"", b""
        with open(path, "rb") as f:
            data = f.read()
        i = 0
        try:
            while i < len(data):
                fields = []
                i += 2  # family
                for _ in range(4):
                    n = struct.unpack(">H", data[i:i + 2])[0]
                    fields.append(data[i + 2:i + 2 + n])
                    i += 2 + n
                _addr, dnum, name, cookie = fields
                if dnum.decode() in (num, "") and name == b"MIT-MAGIC-COOKIE-1":
                    return name, cookie
        except (struct.error, UnicodeDecodeError):
            pass
        return b"", b""

    def _setup(self, auth_name, auth_data):
        req = struct.pack("<BxHHHHxx", ord("l"), 11, 0, len(auth_name), len(auth_data))
        req += auth_name + b"\0" * _pad(len(auth_name)) + auth_data + b"\0" * _pad(len(auth_data))
        self.sock.sendall(req)
        head = self._read(8)
        status, reason_len, _maj, _min, extra = struct.unpack("<BBHHH", head)
        body = self._read(extra * 4)
        if status != 1:
            raise XError("X connection refused: %r" % body[:reason_len])
        (self.id_base, self.id_mask) = struct.unpack("<II", body[4:12])
        vendor_len, = struct.unpack("<H", body[16:18])
        n_screens, n_formats = struct.unpack("<BB", body[20:22])
        off = 32 + vendor_len + _pad(vendor_len) + 8 * n_formats
        self.root, = struct.unpack("<I", body[off:off + 4])
        self.width, self.height = struct.unpack("<HH", body[off + 20:off + 24])
        self.next_id = 1

    def _read(self, n):
        buf = b""
        while len(buf) < n:
            chunk = self.sock.recv(n - len(buf))
            if not chunk:
                raise XError("X connection closed")
            buf += chunk
        return buf

    def _send(self, data):
        self.seq = (self.seq + 1) & 0xFFFF
        self.sock.sendall(data)
        return self.seq

    def _reply(self, seq):
        """Read until the reply for `seq`; raise on an error for it."""
        while True:
            pkt = self._read(32)
            kind = pkt[0]
            pseq, = struct.unpack("<H", pkt[2:4])
            if kind == 0:
                if pseq == seq:
                    raise XError("X error code %d for request seq %d" % (pkt[1], seq))
                continue
            if kind == 1:
                extra, = struct.unpack("<I", pkt[4:8])
                data = pkt + (self._read(extra * 4) if extra else b"")
                if pseq == seq:
                    return data
            # events: ignore

    def new_id(self):
        wid = self.id_base | (self.next_id & self.id_mask)
        self.next_id += 1
        return wid

    def sync(self):
        self._reply(self._send(struct.pack("<BxH", X_GET_INPUT_FOCUS, 1)))

    # --- queries used to find the Library's window ---

    def children(self, window):
        r = self._reply(self._send(struct.pack("<BxHI", X_QUERY_TREE, 2, window)))
        n, = struct.unpack("<H", r[16:18])
        return list(struct.unpack("<%dI" % n, r[32:32 + 4 * n]))

    def name(self, window):
        r = self._reply(self._send(struct.pack("<BBHIIIII", X_GET_PROPERTY, 0, 6, window,
                                               ATOM_WM_NAME, 0, 0, 256)))
        length, = struct.unpack("<I", r[16:20])
        return r[32:32 + length].decode("utf-8", "replace")

    def viewable(self, window):
        r = self._reply(self._send(struct.pack("<BxHI", X_GET_WINDOW_ATTRIBUTES, 2, window)))
        return r[26] == 2   # map-state: 0 unmapped, 1 unviewable, 2 viewable

    def geometry(self, window):
        r = self._reply(self._send(struct.pack("<BxHI", X_GET_GEOMETRY, 2, window)))
        x, y, w, h = struct.unpack("<hhHH", r[12:20])
        return x, y, w, h

    def find_window(self, needle):
        """Geometry of the first viewable top-level window whose name contains needle."""
        for w in self.children(self.root):
            try:
                if needle in self.name(w) and self.viewable(w):
                    return self.geometry(w)
            except XError:
                continue
        return None

    # --- the shield ---

    def shield(self, x, y, w, h):
        wid = self.new_id()
        mask = CW_BACK_PIXMAP | CW_OVERRIDE_REDIRECT
        values = struct.pack("<II", 0, 1)   # background None, override-redirect True
        req = struct.pack("<BBHIIhhHHHHII", X_CREATE_WINDOW, 0, 8 + 2, wid, self.root,
                          x, y, w, h, 0, 1, 0, mask) + values
        self._send(req)
        self._send(struct.pack("<BxHI", X_MAP_WINDOW, 2, wid))
        # stack-mode Above, in case something else is override-redirect too
        self._send(struct.pack("<BxHIHxxI", X_CONFIGURE_WINDOW, 4, wid, 0x40, 0))
        self.sync()
        return wid

    def unmap(self, wid):
        self._send(struct.pack("<BxHI", X_UNMAP_WINDOW, 2, wid))
        self.sync()

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass
