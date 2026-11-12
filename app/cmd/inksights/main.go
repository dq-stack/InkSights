// InkSights shows the Reading Insights card over the Kindle Library.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dump":
			os.Exit(dump(os.Args[2:]))
		case "probe":
			os.Exit(probe())
		case "render":
			os.Exit(render(os.Args[2:]))
		case "version":
			fmt.Println(version)
			return
		}
	}
	os.Exit(run())
}

var version = "dev"
