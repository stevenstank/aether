// Aether's rand_getrandom.go of gc's crypto/internal/sysrand (v2
// evaluation): getrandom(2) through Aether's syscall.Syscall (D-051), in
// place of internal/syscall/unix's, with gc's fallback to /dev/urandom.

package sysrand

import (
	"math"
	"os"
	"syscall"
	"unsafe"
)

const sysGetrandom = 318 // linux/amd64

func read(b []byte) error {
	for len(b) > 0 {
		size := min(len(b), math.MaxInt32)
		n, _, errno := syscall.Syscall(sysGetrandom, uintptr(unsafe.Pointer(&b[0])), uintptr(size), 0)
		switch errno {
		case 0:
		case syscall.ENOSYS:
			// getrandom(2) is not available before Linux 3.17.
			return urandomRead(b)
		case syscall.EINTR:
			continue
		default:
			return errno
		}
		b = b[n:]
	}
	return nil
}

// aetherFatal is the body of fatal, which gc's runtime implements: it
// writes gc's fatal error and ends the program with status 2, running no
// deferred calls.
func aetherFatal(s string) {
	println("fatal error:", s)
	os.Exit(2)
}
