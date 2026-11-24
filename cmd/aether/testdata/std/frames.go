package main

import (
	"fmt"
	"runtime"
)

// The zero Frames has no frames (Batch 1); Callers and CallersFrames are
// declared but unsupported if reached.
func main() {
	var fs runtime.Frames
	f, more := fs.Next()
	fmt.Println(f, more)
	fmt.Printf("%+v\n", f)
	var fn *runtime.Func
	fmt.Println(fn == nil, f.Func == nil)
}
