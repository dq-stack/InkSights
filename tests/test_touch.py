import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import touch  # noqa: E402

# /proc/bus/input/devices from Dan's PW2 (FW 5.12.2.2)
PW2_DEVICES = """I: Bus=0019 Vendor=0001 Product=0001 Version=0100
N: Name="max77696-onkey"
H: Handlers=kbd event0 
B: PROP=0
B: EV=3
B: KEY=100000 0 0 0

I: Bus=0018 Vendor=0000 Product=0000 Version=0000
N: Name="cyttsp4_mt"
H: Handlers=event1 
B: PROP=0
B: EV=f
B: KEY=6420 0 0 0 0 0 0 0 0 0 0
B: ABS=2658000 3
"""

SAMPLE = os.path.join(os.path.dirname(__file__), "..", "sample", "touch.bin")


class TouchTests(unittest.TestCase):
    def test_finds_touchscreen_not_power_button(self):
        self.assertEqual(touch.touch_devices(PW2_DEVICES), ["/dev/input/event1"])

    @unittest.skipUnless(os.path.exists(SAMPLE), "no local touch recording")
    def test_real_taps_from_probe3(self):
        # recorded on a 32-bit Kindle: 16-byte input_event
        if touch.EVENT.size != 16:
            import struct
            touch.EVENT = struct.Struct("<iiHHi")
        path = os.path.join(tempfile.mkdtemp(), "events")
        with open(SAMPLE, "rb") as f:
            data = f.read()
        t = touch.Touch.__new__(touch.Touch)
        t.x = t.y = None
        t._buf = {}
        taps = []
        for i in range(0, len(data), 64):   # feed in odd-sized chunks
            with open(path, "wb") as f:
                f.write(data[i:i + 64])
            fd = os.open(path, os.O_RDONLY)
            tap = t.read(fd)
            os.close(fd)
            if tap:
                taps.append(tap)
        # top-left corner, bottom-right corner, Home icon (menu bar ends at y=116)
        self.assertEqual(taps, [(49, 28), (730, 984), (52, 93)])
        self.assertTrue(taps[2][1] < 116)


if __name__ == "__main__":
    unittest.main()
