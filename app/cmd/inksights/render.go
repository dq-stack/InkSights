package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"github.com/dq-stack/InkSights/app/internal/books"
	"github.com/dq-stack/InkSights/app/internal/card"
	"github.com/dq-stack/InkSights/app/internal/clock"
	"github.com/dq-stack/InkSights/app/internal/history"
	"github.com/dq-stack/InkSights/app/internal/stats"
)

// render draws the card from a reading log into a PNG on a white page, for
// checking the design on a computer.
func render(args []string) int {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	log := fs.String("log", "snapshots.jsonl", "reading log")
	fonts := fs.String("fonts", "", "folder with Amazon-Ember-Regular.ttf and Amazon-Ember-Bold.ttf (default: the Kindle's)")
	width := fs.Int("width", 758, "screen width to scale for")
	day := fs.String("today", "", "date to draw for (2006-01-02)")
	out := fs.String("out", "card.png", "output PNG")
	libH := fs.Int("libh", 0, "height of the Library area to fit (0 = no limit)")
	fs.Parse(args)

	today := clock.Date(clock.Now())
	if *day != "" {
		t, ok := clock.Parse(*day + "T00:00:00")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad -today")
			return 2
		}
		today = t
	}
	reg, bold := card.KindleFonts[0], card.KindleFonts[1]
	if *fonts != "" {
		reg = filepath.Join(*fonts, "Amazon-Ember-Regular.ttf")
		bold = filepath.Join(*fonts, "Amazon-Ember-Bold.ttf")
	}
	scale := float64(*width) / 758
	f, err := card.LoadFonts(reg, bold, scale)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s := stats.Compute(history.Load(*log), today, []books.Book(nil))
	img := card.Draw(s, today, f, scale)
	if need := float64(img.Rect.Dy()) + (cardGap+bottomGap+6)*scale; *libH > 0 && need > float64(*libH) {
		scale *= float64(*libH) / need
		f, _ = card.LoadFonts(reg, bold, scale)
		img = card.Draw(s, today, f, scale)
	}
	fmt.Printf("scale %.3f, card %dx%d\n", scale, img.Rect.Dx(), img.Rect.Dy())
	framed := card.Frame(img, scale)
	page := image.NewGray(image.Rect(0, 0, framed.Rect.Dx()+40, framed.Rect.Dy()+40))
	draw.Draw(page, page.Rect, image.NewUniform(color.Gray{0xFF}), image.Point{}, draw.Src)
	draw.Draw(page, framed.Rect.Add(image.Pt(20, 20)), framed, image.Point{}, draw.Over)
	o, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer o.Close()
	if err := png.Encode(o, page); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
