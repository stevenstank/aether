package main

import (
	"fmt"
	"os"
	"text/tabwriter"
)

func main() {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "a\tbbb\t")
	tw.Flush()
}
