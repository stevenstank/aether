// The bodies of the functions of gc's crypto/internal/fips140 that gc's
// runtime implements (v2 evaluation).

package fips140

import "os"

// aetherIndicator is the FIPS service indicator. gc keeps one per
// goroutine; Aether's goroutines switch only in channel operations
// (docs/SEMANTICS.md V-10), and the module sets and reads the indicator
// with none in between, so one serves every goroutine alike.
var aetherIndicator uint8

func aetherGetIndicator() uint8  { return aetherIndicator }
func aetherSetIndicator(i uint8) { aetherIndicator = i }

// aetherFatal writes gc's fatal error and ends the program with status 2,
// running no deferred calls.
func aetherFatal(s string) {
	println("fatal error:", s)
	os.Exit(2)
}
