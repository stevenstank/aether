// Closing a standard file: a later write fails, and the file opened next
// takes the lowest free descriptor, as the kernel numbers them. (gc's
// runtime then opens descriptors of its own, so later numbers differ.)
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, os.Stdin.Close())
	f, err := os.Open("in.txt")
	fmt.Fprintln(os.Stderr, f.Fd(), err)
	b := make([]byte, 9)
	n, err := f.Read(b)
	fmt.Fprintln(os.Stderr, n, err, string(b))
	fmt.Println("to stdout")
	fmt.Fprintln(os.Stderr, os.Stdout.Close())
	_, err = fmt.Println("lost")
	fmt.Fprintln(os.Stderr, err)
	_, err = os.Stdout.Write(nil)
	fmt.Fprintln(os.Stderr, err)
	os.Exit(3)
}
