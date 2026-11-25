// zone prints the local zone gc's time finds in the environment (TZ,
// ZONEINFO), or, given an argument, after os.Setenv("TZ", it) before time
// first uses it: time and os share syscall's environment (D-054).
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		os.Setenv("TZ", os.Args[1])
	}
	t := time.Unix(1772360102, 0)
	name, off := t.Zone()
	fmt.Println(time.Local, t, name, off, t.Format(time.RFC1123))
}
