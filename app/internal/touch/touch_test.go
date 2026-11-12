package touch

import (
	"reflect"
	"testing"
)

// /proc/bus/input/devices from Dan's PW2 (FW 5.12.2.2)
const pw2 = `I: Bus=0019 Vendor=0001 Product=0001 Version=0100
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
`

// ... and from his Kindle 11th gen (FW 5.18.1)
const k11 = `I: Bus=0019 Vendor=0001 Product=0001 Version=0100
N: Name="bd71828-pwrkey"
H: Handlers=event0 perfmgr
B: PROP=0
B: EV=3
B: KEY=100000 0 0 0

I: Bus=0000 Vendor=beef Product=dead Version=28bb
N: Name="goodix-ts"
H: Handlers=event1 perfmgr
B: PROP=2
B: EV=b
B: KEY=420 0 0 0 0 0 0 100000 0 0 0
B: ABS=2618000 3
`

func TestFindsTouchscreenNotPowerButton(t *testing.T) {
	for name, text := range map[string]string{"pw2": pw2, "k11": k11} {
		if got := Devices(text); !reflect.DeepEqual(got, []string{"/dev/input/event1"}) {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
