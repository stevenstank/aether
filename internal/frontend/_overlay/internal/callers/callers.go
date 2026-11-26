// Package runtime, at internal/callers, is what flag and log use of gc's
// runtime in Aether (v2 evaluation): their callers' positions, which
// Aether's binaries carry no tables to find. Caller reports none, which
// its contract allows ("it was not possible to recover the information"),
// so flag's message for a flag set before its definition has "?:0", and
// log's Lshortfile and Llongfile prefixes are "???:0". A program's own
// runtime.Caller stays unsupported.
package runtime

import rt "runtime"

// Caller reports no caller.
func Caller(skip int) (pc uintptr, file string, line int, ok bool) { return 0, "", 0, false }

// CallersFrames returns Frames that have no frame.
func CallersFrames(callers []uintptr) *rt.Frames { return &rt.Frames{} }
