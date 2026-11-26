// Aether's check.go of gc's crypto/internal/fips140/check (v2
// evaluation). gc's linker writes Linkinfo, through //go:linkname, with the
// checksum of the FIPS module's code and data, which init verifies in FIPS
// mode (GODEBUG=fips140=on). Aether's binaries carry no such checksum, so
// in FIPS mode init fails as gc's does when none is found; outside it, as
// by default, nothing is verified, as in gc.

// Package check implements the power-on self-test of the FIPS module.
package check

import (
	"crypto/internal/fips140"
	"unsafe"
)

// Verified is set when verification succeeded. It can be expected to always
// be true when [fips140.Enabled] is true, or init would have panicked.
var Verified bool

// Linkinfo holds the go:fipsinfo symbol prepared by gc's linker; Aether's
// is never filled.
var Linkinfo struct {
	Magic [16]byte
	Sum   [32]byte
	Self  uintptr
	Sects [4]struct {
		Start unsafe.Pointer
		End   unsafe.Pointer
	}
}

func init() {
	if !fips140.Enabled {
		return
	}
	if err := fips140.Supported(); err != nil {
		panic("fips140: " + err.Error())
	}
	panic("fips140: no verification checksum found")
}
