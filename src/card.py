"""Draw the Reading Insights card with Pillow.

Shared by the Kindle app and the Mac mockup tool so what Dan approved is
what the device shows. Sizes are designed for the PW2 (758 px wide, 212 ppi)
and scaled by `scale` for other screens.
"""

from datetime import datetime

from PIL import Image, ImageDraw, ImageFont

INK, MID, LIGHT, PAPER = 0, 0x77, 0xCC, 0xFF
HEAT = [PAPER, 0xDD, 0xAA, 0x66, 0x00]
MONTHS = "JFMAMJJASOND"
CARD_W = 660
RADIUS = 18

KINDLE_FONTS = ("/usr/java/lib/fonts/Amazon-Ember-Regular.ttf",
                "/usr/java/lib/fonts/Amazon-Ember-Bold.ttf")


class Fonts(object):
    """(regular, bold) font files; a .ttc pair uses face index 0/1."""

    def __init__(self, regular, bold, ttc_index=False, scale=1.0):
        self.paths = (regular, bold)
        self.ttc_index = ttc_index
        self.scale = scale
        self._cache = {}

    def __call__(self, size, bold=False):
        key = (size, bold)
        if key not in self._cache:
            size = int(round(size * self.scale))
            if self.ttc_index:
                f = ImageFont.truetype(self.paths[0], size, index=1 if bold else 0)
            else:
                f = ImageFont.truetype(self.paths[1 if bold else 0], size)
            self._cache[key] = f
        return self._cache[key]


def hours(ms, short=False):
    m = int(round(ms / 60000.0))
    if m < 60:
        return "%dm" % m
    h, m = divmod(m, 60)
    return "%dh" % h if short or m == 0 else "%dh %02dm" % (h, m)


def plural(n, word):
    return "%d %s%s" % (n, word, "" if n == 1 else "s")


def ellipsize(d, text, f, width):
    if d.textlength(text, font=f) <= width:
        return text
    while text and d.textlength(text + "…", font=f) > width:
        text = text[:-1]
    return text.rstrip() + "…"


def draw_card(s, today, font, scale=1.0):
    """Return the card as an "L" image (square corners; see `frame`)."""
    k = lambda v: int(round(v * scale))   # noqa: E731
    W = k(CARD_W)
    pad = k(32)
    inner = W - 2 * pad
    img = Image.new("L", (W, k(1100)), PAPER)
    d = ImageDraw.Draw(img)
    y = pad

    # header
    d.text((pad, y), "Reading Insights", font=font(36, True), fill=INK)
    d.text((W - pad, y + k(12)), "%d %s" % (today.day, today.strftime("%b %Y")),
           font=font(20), fill=MID, anchor="ra")
    y += k(54)
    d.line((pad, y, W - pad, y), fill=INK, width=max(1, k(2)))
    y += k(18)

    # three stat tiles
    tiles = [("TODAY", hours(s["today_ms"]), ""),
             ("STREAK", plural(s["streak"], "day"), "best %d" % s["best_streak"]),
             ("TOTAL", hours(s["lifetime_ms"], short=True), plural(s["books"], "book"))]
    tw = inner // 3
    for i, (label, value, sub) in enumerate(tiles):
        x = pad + i * tw
        if i:
            d.line((x, y + k(4), x, y + k(92)), fill=LIGHT, width=max(1, k(2)))
        cx = x + tw // 2
        d.text((cx, y), label, font=font(17, True), fill=MID, anchor="ma")
        d.text((cx, y + k(26)), value, font=font(38, True), fill=INK, anchor="ma")
        if sub:
            d.text((cx, y + k(72)), sub, font=font(18), fill=MID, anchor="ma")
    y += k(104)

    # hours by month
    d.text((pad, y), "Hours by month · %d" % today.year, font=font(20, True), fill=INK)
    y += k(34)
    chart_h = k(64)
    bw = inner / 12.0
    top = max(s["monthly_ms"]) or 1
    for m, ms in enumerate(s["monthly_ms"]):
        x0 = pad + m * bw + k(6)
        x1 = pad + (m + 1) * bw - k(6)
        h = int(chart_h * ms / top)
        if ms and h < 3:
            h = 3
        current = m == today.month - 1
        if h:
            d.rectangle((x0, y + chart_h - h, x1, y + chart_h), fill=INK if current else MID)
        d.text(((x0 + x1) / 2, y + chart_h + k(6)), MONTHS[m], font=font(16),
               fill=INK if current else MID, anchor="ma")
    d.line((pad, y + chart_h, W - pad, y + chart_h), fill=INK, width=1)
    y += chart_h + k(36)

    # most read
    d.text((pad, y), "Most read", font=font(20, True), fill=INK)
    d.text((W - pad, y + k(2)), "%d finished" % s["finished"], font=font(18), fill=MID, anchor="ra")
    y += k(36)
    bar_w = inner - k(110)
    for b in s["top"][:4]:
        d.text((pad, y), ellipsize(d, b["title"], font(22), bar_w), font=font(22), fill=INK)
        d.text((W - pad, y), hours(b["ms"]), font=font(22, True), fill=INK, anchor="ra")
        y += k(32)
        pct = b["percent"]
        d.rounded_rectangle((pad, y, pad + bar_w, y + k(8)), radius=k(4), outline=MID, width=1)
        if pct:
            d.rounded_rectangle((pad, y, pad + max(k(8), int(bar_w * min(pct, 1.0))), y + k(8)),
                                radius=k(4), fill=INK)
        d.text((pad + bar_w + k(10), y - k(6)), "%d%%" % round(pct * 100) if pct is not None else "–",
               font=font(16), fill=MID)
        y += k(20)
    if not s["top"]:
        d.text((pad, y), "Open a book to start tracking", font=font(20), fill=MID)
        y += k(32)
    y += k(12)

    # heatmap: last 26 weeks, columns = weeks, rows = Mon..Sun
    d.text((pad, y), "Last 26 weeks", font=font(20, True), fill=INK)
    y += k(34)
    grid = s["heatmap"]
    cell = inner // len(grid)
    sq = cell - k(3)
    cell_v = k(15)
    for w, col in enumerate(grid):
        for dow, lvl in enumerate(col):
            if lvl is None:
                continue
            x0 = pad + w * cell
            y0 = y + dow * cell_v
            d.rectangle((x0, y0, x0 + sq, y0 + cell_v - k(3)), fill=HEAT[lvl],
                        outline=LIGHT if lvl == 0 else None)
    y += 7 * cell_v + k(14)

    since = s["since"]
    if since:
        sd = datetime.strptime(since, "%Y-%m-%d")
        foot = "Tracking since %d %s" % (sd.day, sd.strftime("%b %Y"))
    else:
        foot = "Tracking starts today"
    d.text((pad, y), foot, font=font(17), fill=MID)
    d.text((W - pad, y), "Tap to close", font=font(17), fill=MID, anchor="ra")
    y += k(24) + pad - k(8)

    return img.crop((0, 0, W, y))


def frame(card, behind, scale=1.0):
    """Round the card's corners, add a border and drop shadow, over `behind`
    (an "L" image of the screen area the card covers, extended by the shadow
    offset). Returns the image to put on screen at the same origin as `behind`."""
    k = lambda v: int(round(v * scale))   # noqa: E731
    out = behind.copy()
    r = k(RADIUS)
    off = k(6)
    mask = Image.new("L", card.size, 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, card.width - 1, card.height - 1), radius=r, fill=255)
    out.paste(Image.new("L", card.size, INK), (off, off), mask)
    out.paste(card, (0, 0), mask)
    ImageDraw.Draw(out).rounded_rectangle((0, 0, card.width - 1, card.height - 1), radius=r,
                                          outline=INK, width=max(2, k(3)))
    return out


def see_through_runs(card, scale=1.0):
    """Where the screen behind shows through the framed card (outside the
    rounded corners and around the shadow), as (row, start, length) runs over
    the shadow_size() area. Lets a saved card be redrawn over a Library that
    has changed since, without Pillow."""
    import re
    k = lambda v: int(round(v * scale))   # noqa: E731
    w, h = shadow_size(card, scale)
    r, off = k(RADIUS), k(6)
    mask = Image.new("L", (w, h), 0)
    d = ImageDraw.Draw(mask)
    d.rounded_rectangle((off, off, off + card.width - 1, off + card.height - 1), radius=r, fill=255)
    d.rounded_rectangle((0, 0, card.width - 1, card.height - 1), radius=r, fill=255)
    raw = mask.tobytes()
    runs = []
    for row in range(h):
        line = raw[row * w:(row + 1) * w]
        for m in re.finditer(b"\x00+", line):
            runs.append((row, m.start(), m.end() - m.start()))
    return runs


def shadow_size(card, scale=1.0):
    off = int(round(6 * scale))
    return card.width + off, card.height + off
