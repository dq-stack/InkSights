package main

import (
	"fmt"
	"os"

	"github.com/dq-stack/InkSights/app/internal/screen"
	"github.com/dq-stack/InkSights/app/internal/touch"
	"github.com/dq-stack/InkSights/app/internal/xshield"
)

// probe reports what InkSights finds on this Kindle without drawing anything.
func probe() int {
	scr, err := screen.Open()
	if err != nil {
		fmt.Println("screen:", err)
		return 1
	}
	defer scr.Close()
	fmt.Printf("screen: %dx%d, fbink %s\n", scr.Width, scr.Height, scr.FBInk)
	fmt.Printf("framebuffer readable: %v\n", scr.Grab(0, 0, 4, 4) != nil)
	xd, err := xshield.Open(os.Getenv("DISPLAY"))
	if err != nil {
		fmt.Println("X:", err)
	} else {
		top, h := libraryArea(xd, scr.Width, scr.Height)
		fmt.Printf("library area: y=%d h=%d (X screen %dx%d)\n", top, h, xd.Width, xd.Height)
		xd.Close()
	}
	text, _ := os.ReadFile("/proc/bus/input/devices")
	fmt.Printf("touch devices: %v\n", touch.Devices(string(text)))
	return 0
}
