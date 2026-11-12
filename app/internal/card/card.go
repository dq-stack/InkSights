// Package card draws the Reading Insights card.
//
// Sizes are designed for the Paperwhite 2 (758 px wide) and scaled for
// other screens. The finished card is a grey+alpha image: transparent
// outside its rounded corners and drop shadow, so FBInk blends it over
// whatever the Library shows underneath.
package card

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/dq-stack/InkSights/app/internal/stats"
)

// Greys used on the card.
const (
	Ink   = 0x00
	Mid   = 0x77
	Light = 0xCC
	Paper = 0xFF
)

var heat = []uint8{Paper, 0xDD, 0xAA, 0x66, 0x00}

const (
	months = "JFMAMJJASOND"
	// Width is the card's width on a 758 px wide screen.
	Width  = 660
	radius = 18
	shadow = 6
)

// KindleFonts are the Kindle's own Amazon Ember fonts (regular, bold).
var KindleFonts = [2]string{"/usr/java/lib/fonts/Amazon-Ember-Regular.ttf",
	"/usr/java/lib/fonts/Amazon-Ember-Bold.ttf"}

// Fonts loads faces at design sizes, scaled.
type Fonts struct {
	reg, bold *opentype.Font
	scale     float64
	cache     map[[2]int]font.Face
}

// LoadFonts reads a regular and a bold TrueType font.
func LoadFonts(regular, bold string, scale float64) (*Fonts, error) {
	load := func(p string) (*opentype.Font, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		return opentype.Parse(data)
	}
	r, err := load(regular)
	if err != nil {
		return nil, err
	}
	b, err := load(bold)
	if err != nil {
		return nil, err
	}
	return &Fonts{reg: r, bold: b, scale: scale, cache: map[[2]int]font.Face{}}, nil
}

func (f *Fonts) face(size int, bold bool) font.Face {
	key := [2]int{size, 0}
	src := f.reg
	if bold {
		key[1] = 1
		src = f.bold
	}
	if fc, ok := f.cache[key]; ok {
		return fc
	}
	px := math.Round(float64(size) * f.scale)
	fc, err := opentype.NewFace(src, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	f.cache[key] = fc
	return fc
}

// Hours formats reading time: "45m", "3h 07m", or "3h" when short.
func Hours(ms int64, short bool) string {
	m := int(math.RoundToEven(float64(ms) / 60000))
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	h, m := m/60, m%60
	if short || m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %02dm", h, m)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// canvas is a grey image with Pillow-like drawing helpers.
type canvas struct {
	img *image.Gray
}

func (c *canvas) set(x, y int, v uint8) {
	if image.Pt(x, y).In(c.img.Rect) {
		c.img.SetGray(x, y, color.Gray{v})
	}
}

// rect fills x0..x1, y0..y1 inclusive (Pillow's rectangle).
func (c *canvas) rect(x0, y0, x1, y1 int, v uint8) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c.set(x, y, v)
		}
	}
}

// outlineRect draws a 1 px outline around x0..x1, y0..y1 inclusive.
func (c *canvas) outlineRect(x0, y0, x1, y1 int, v uint8) {
	for x := x0; x <= x1; x++ {
		c.set(x, y0, v)
		c.set(x, y1, v)
	}
	for y := y0; y <= y1; y++ {
		c.set(x0, y, v)
		c.set(x1, y, v)
	}
}

// hline draws a horizontal line of width w (like Pillow's line on y).
func (c *canvas) hline(x0, x1, y, w int, v uint8) {
	top := y - (w-1)/2
	c.rect(x0, top, x1, top+w-1, v)
}

// vline draws a vertical line of width w.
func (c *canvas) vline(x, y0, y1, w int, v uint8) {
	left := x - (w-1)/2
	c.rect(left, y0, left+w-1, y1, v)
}

// inRounded reports whether pixel (x, y) is inside the rounded rectangle
// x0..x1, y0..y1 inclusive with corner radius r.
func inRounded(x, y, x0, y0, x1, y1, r int) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	if r <= 0 {
		return true
	}
	cx, cy := -1, -1
	switch {
	case x < x0+r && y < y0+r:
		cx, cy = x0+r, y0+r
	case x > x1-r && y < y0+r:
		cx, cy = x1-r, y0+r
	case x < x0+r && y > y1-r:
		cx, cy = x0+r, y1-r
	case x > x1-r && y > y1-r:
		cx, cy = x1-r, y1-r
	default:
		return true
	}
	dx, dy := float64(x-cx), float64(y-cy)
	return dx*dx+dy*dy <= (float64(r)+0.5)*(float64(r)+0.5)
}

// roundedFill fills a rounded rectangle.
func (c *canvas) roundedFill(x0, y0, x1, y1, r int, v uint8) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if inRounded(x, y, x0, y0, x1, y1, r) {
				c.set(x, y, v)
			}
		}
	}
}

// roundedOutline draws a rounded rectangle's outline, w px wide, inward.
func (c *canvas) roundedOutline(x0, y0, x1, y1, r, w int, v uint8) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if inRounded(x, y, x0, y0, x1, y1, r) && !inRounded(x, y, x0+w, y0+w, x1-w, y1-w, r-w) {
				c.set(x, y, v)
			}
		}
	}
}

func textWidth(f font.Face, s string) int {
	return font.MeasureString(f, s).Round()
}

// text draws s with its top (ascender line) at y. anchor is "l" (x is the
// left edge), "r" (right edge) or "m" (middle), like Pillow's la/ra/ma.
func (c *canvas) text(x, y int, s string, f font.Face, v uint8, anchor byte) {
	w := textWidth(f, s)
	switch anchor {
	case 'r':
		x -= w
	case 'm':
		x -= w / 2
	}
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(color.Gray{v}), Face: f,
		Dot: fixed.P(x, y+f.Metrics().Ascent.Round())}
	d.DrawString(s)
}

func ellipsize(s string, f font.Face, width int) string {
	if textWidth(f, s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && textWidth(f, string(r)+"…") > width {
		r = r[:len(r)-1]
	}
	return strings.TrimRight(string(r), " ") + "…"
}

// Draw returns the card (square corners; see Frame).
func Draw(s stats.Stats, today time.Time, fonts *Fonts, scale float64) *image.Gray {
	k := func(v float64) int { return int(math.RoundToEven(v * scale)) }
	ft := func(size int, bold bool) font.Face { return fonts.face(size, bold) }
	W := k(Width)
	pad := k(32)
	inner := W - 2*pad
	c := &canvas{img: image.NewGray(image.Rect(0, 0, W, k(1100)))}
	draw.Draw(c.img, c.img.Rect, image.NewUniform(color.Gray{Paper}), image.Point{}, draw.Src)
	y := pad

	// header
	c.text(pad, y, "Reading Insights", ft(36, true), Ink, 'l')
	c.text(W-pad, y+k(12), fmt.Sprintf("%d %s", today.Day(), today.Format("Jan 2006")), ft(20, false), Mid, 'r')
	y += k(54)
	c.hline(pad, W-pad, y, max(1, k(2)), Ink)
	y += k(18)

	// three stat tiles
	tiles := [][3]string{
		{"TODAY", Hours(s.TodayMs, false), ""},
		{"STREAK", plural(s.Streak, "day"), fmt.Sprintf("best %d", s.BestStreak)},
		{"TOTAL", Hours(s.LifetimeMs, true), plural(s.Books, "book")},
	}
	tw := inner / 3
	for i, t := range tiles {
		x := pad + i*tw
		if i > 0 {
			c.vline(x, y+k(4), y+k(92), max(1, k(2)), Light)
		}
		cx := x + tw/2
		c.text(cx, y, t[0], ft(17, true), Mid, 'm')
		c.text(cx, y+k(26), t[1], ft(38, true), Ink, 'm')
		if t[2] != "" {
			c.text(cx, y+k(72), t[2], ft(18, false), Mid, 'm')
		}
	}
	y += k(104)

	// hours by month
	c.text(pad, y, fmt.Sprintf("Hours by month · %d", today.Year()), ft(20, true), Ink, 'l')
	y += k(34)
	chartH := k(64)
	bw := float64(inner) / 12
	top := int64(1)
	for _, ms := range s.MonthlyMs {
		if ms > top {
			top = ms
		}
	}
	for m, ms := range s.MonthlyMs {
		x0 := int(float64(pad) + float64(m)*bw + float64(k(6)))
		x1 := int(float64(pad) + float64(m+1)*bw - float64(k(6)))
		h := int(int64(chartH) * ms / top)
		if ms > 0 && h < 3 {
			h = 3
		}
		current := m == int(today.Month())-1
		shade := uint8(Mid)
		if current {
			shade = Ink
		}
		if h > 0 {
			c.rect(x0, y+chartH-h, x1, y+chartH, shade)
		}
		c.text((x0+x1)/2, y+chartH+k(6), months[m:m+1], ft(16, false), shade, 'm')
	}
	c.hline(pad, W-pad, y+chartH, 1, Ink)
	y += chartH + k(36)

	// most read
	c.text(pad, y, "Most read", ft(20, true), Ink, 'l')
	c.text(W-pad, y+k(2), fmt.Sprintf("%d finished", s.Finished), ft(18, false), Mid, 'r')
	y += k(36)
	barW := inner - k(110)
	n := len(s.Top)
	if n > 4 {
		n = 4
	}
	for _, b := range s.Top[:n] {
		c.text(pad, y, ellipsize(b.Title, ft(22, false), barW), ft(22, false), Ink, 'l')
		c.text(W-pad, y, Hours(b.Ms, false), ft(22, true), Ink, 'r')
		y += k(32)
		c.roundedOutline(pad, y, pad+barW, y+k(8), k(4), 1, Mid)
		label := "–"
		if b.Percent != nil {
			pct := *b.Percent
			if pct > 0 {
				fill := int(float64(barW) * math.Min(pct, 1))
				if fill < k(8) {
					fill = k(8)
				}
				c.roundedFill(pad, y, pad+fill, y+k(8), k(4), Ink)
			}
			label = fmt.Sprintf("%d%%", int(math.RoundToEven(pct*100)))
		}
		c.text(pad+barW+k(10), y-k(6), label, ft(16, false), Mid, 'l')
		y += k(20)
	}
	if len(s.Top) == 0 {
		c.text(pad, y, "Open a book to start tracking", ft(20, false), Mid, 'l')
		y += k(32)
	}
	y += k(12)

	// heatmap: last 26 weeks, columns = weeks, rows = Mon..Sun
	c.text(pad, y, "Last 26 weeks", ft(20, true), Ink, 'l')
	y += k(34)
	cell := inner / len(s.Heatmap)
	sq := cell - k(3)
	cellV := k(15)
	for w, col := range s.Heatmap {
		for dow, lvl := range col {
			if lvl < 0 {
				continue
			}
			x0 := pad + w*cell
			y0 := y + dow*cellV
			c.rect(x0, y0, x0+sq, y0+cellV-k(3), heat[lvl])
			if lvl == 0 {
				c.outlineRect(x0, y0, x0+sq, y0+cellV-k(3), Light)
			}
		}
	}
	y += 7*cellV + k(14)

	foot := "Tracking starts today"
	if s.Since != "" {
		if sd, err := time.Parse("2006-01-02", s.Since); err == nil {
			foot = fmt.Sprintf("Tracking since %d %s", sd.Day(), sd.Format("Jan 2006"))
		}
	}
	c.text(pad, y, foot, ft(17, false), Mid, 'l')
	c.text(W-pad, y, "Tap to close", ft(17, false), Mid, 'r')
	y += k(24) + pad - k(8)

	return c.img.SubImage(image.Rect(0, 0, W, y)).(*image.Gray)
}

// Frame rounds the card's corners and adds a border and drop shadow. The
// result is grey+alpha: transparent wherever the screen behind should show.
func Frame(card *image.Gray, scale float64) *image.NRGBA {
	k := func(v float64) int { return int(math.RoundToEven(v * scale)) }
	r, off := k(radius), k(shadow)
	cw, ch := card.Rect.Dx(), card.Rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, cw+off, ch+off))
	put := func(x, y int, v uint8) {
		i := out.PixOffset(x, y)
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = v, v, v, 0xFF
	}
	// shadow: the card's shape offset down-right, solid black
	for y := off; y < ch+off; y++ {
		for x := off; x < cw+off; x++ {
			if inRounded(x, y, off, off, off+cw-1, off+ch-1, r) {
				put(x, y, Ink)
			}
		}
	}
	bw := max(2, k(3))
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			if !inRounded(x, y, 0, 0, cw-1, ch-1, r) {
				continue
			}
			v := card.GrayAt(card.Rect.Min.X+x, card.Rect.Min.Y+y).Y
			if !inRounded(x, y, bw, bw, cw-1-bw, ch-1-bw, r-bw) {
				v = Ink // border
			}
			put(x, y, v)
		}
	}
	return out
}

// RegionWidth is the width the framed card takes on a screen this many
// pixels wide (card plus shadow), and Left is its x position.
func RegionWidth(scale float64) int {
	return int(math.RoundToEven(Width*scale)) + int(math.RoundToEven(shadow*scale))
}

// Left is the card's x position on a screen of this width.
func Left(screenW int, scale float64) int {
	return (screenW - int(math.RoundToEven(Width*scale))) / 2
}
