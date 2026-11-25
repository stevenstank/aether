package main

import (
	"flag"
	"fmt"
)

func main() {
	n := flag.Int("n", 3, "count")
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	d := fs.Duration("d", 0, "dur")
	fs.Parse([]string{"-d", "1s"})
	flag.Parse()
	fmt.Println(*n, *d, flag.NArg())
}
