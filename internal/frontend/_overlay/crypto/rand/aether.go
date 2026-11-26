// Aether's part of gc's crypto/rand: the runtime's fatal (D-057).

package rand

import "os"

// aetherFatal is the body of fatal, which gc's runtime implements: it
// writes gc's fatal error and ends the program with status 2, running no
// deferred calls.
func aetherFatal(s string) {
	println("fatal error:", s)
	os.Exit(2)
}
