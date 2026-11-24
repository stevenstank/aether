// Declarations of gc's runtime API that Aether does not implement, so that
// the standard-library packages using them type-check (Batch 1): for
// example internal/bisect, which internal/godebug imports, and log and
// flag. Callers, CallersFrames, and Caller have no body and are not
// intrinsics, so a program that reaches one is unsupported
// (function-without-body), as gc's runtime would be needed for it.

package runtime

// A Func represents a Go function in the running binary. Aether declares
// none of its methods.
type Func struct {
	opaque struct{}
}

// Frame is the information returned by Frames for each call frame. Its
// fields are gc's, unexported ones included, so that fmt prints a Frame as
// gc's.
type Frame struct {
	PC        uintptr
	Func      *Func
	Function  string
	File      string
	Line      int
	startLine int
	Entry     uintptr
	funcInfo  funcInfo
}

// funcInfo, _func, and moduledata are opaque here: gc's describe its
// binary's functions.
type funcInfo struct {
	*_func
	datap *moduledata
}

type _func struct{}

type moduledata struct{}

// Frames may be used to get function/file/line information for a slice of
// PC values returned by Callers. Only CallersFrames, which is unsupported,
// makes one other than the zero Frames.
type Frames struct{}

// Next returns a Frame representing the next call frame in the slice of PC
// values, and whether there are more frames. The zero Frames has none: gc's
// returns the zero Frame and false for it, as this does.
func (ci *Frames) Next() (frame Frame, more bool) {
	return Frame{}, false
}

// Callers fills pc with the return program counters of function
// invocations on the calling goroutine's stack.
func Callers(skip int, pc []uintptr) int

// CallersFrames takes a slice of PCs returned by Callers and prepares to
// return function/file/line information.
func CallersFrames(callers []uintptr) *Frames

// Caller reports file and line number information about function
// invocations on the calling goroutine's stack.
func Caller(skip int) (pc uintptr, file string, line int, ok bool)
