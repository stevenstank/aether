// Package runtime, at internal/callers, is what flag, log, and net/http
// use of gc's runtime in Aether (v2 evaluation, D-057): their callers'
// positions and stacks, which Aether's binaries carry no tables to find.
// Caller and Callers report none, which their contract allows ("it was
// not possible to recover the information"), so flag's message for a flag
// set before its definition has "?:0", log's Lshortfile and Llongfile
// prefixes are "???:0", and net/http's message for a handler's panic has
// no stack. A program's own runtime.Caller and Stack stay unsupported.
package runtime

import rt "runtime"

const GOOS = rt.GOOS

type Frame = rt.Frame

// Caller reports no caller.
func Caller(skip int) (pc uintptr, file string, line int, ok bool) { return 0, "", 0, false }

// Callers records no program counter.
func Callers(skip int, pc []uintptr) int { return 0 }

// CallersFrames returns Frames that have no frame.
func CallersFrames(callers []uintptr) *rt.Frames { return &rt.Frames{} }

// Stack writes no trace.
func Stack(buf []byte, all bool) int { return 0 }
