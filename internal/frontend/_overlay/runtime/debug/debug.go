// Package debug is Aether's replacement of gc's runtime/debug (v2
// evaluation): the settings of gc's runtime that a program may change and
// read back. Aether's runtime records them; its stacks (V-2) and its
// collector's trigger (D-046) do not follow them. The rest of gc's package
// is not declared, so a program using it is unsupported.
package debug

import (
	"math"
	"runtime"
)

var (
	maxStack    = 1000000000 // gc's default on 64-bit systems
	gcPercent   = 100
	memoryLimit = int64(math.MaxInt64)
)

// SetMaxStack sets the maximum amount of memory that can be used by a
// single goroutine stack and returns the previous setting. Aether's
// stacks keep their own fixed size (docs/SEMANTICS.md V-2).
func SetMaxStack(bytes int) int {
	prev := maxStack
	maxStack = bytes
	return prev
}

// SetGCPercent sets the garbage collection target percentage and returns
// the previous setting. A negative percentage disables collection in gc;
// Aether's collector runs on its own trigger regardless.
func SetGCPercent(percent int) int {
	prev := gcPercent
	gcPercent = percent
	return prev
}

// SetMemoryLimit provides the runtime with a soft memory limit and returns
// the previous limit. A negative input only reads the current one.
func SetMemoryLimit(limit int64) int64 {
	prev := memoryLimit
	if limit >= 0 {
		memoryLimit = limit
	}
	return prev
}

// FreeOSMemory forces a garbage collection.
func FreeOSMemory() { runtime.GC() }
