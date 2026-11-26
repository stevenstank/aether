// The bodies of gc's crypto/fips140 bypass functions, which gc's runtime
// implements per goroutine (v2 evaluation). They matter only when
// enforcement is on (GODEBUG=fips140=only), which also enables FIPS mode,
// in which an Aether program stops at its FIPS check's init
// (crypto/internal/fips140/check): no program that runs observes them.

package fips140

var aetherBypass int

func aetherSetBypass()       { aetherBypass++ }
func aetherUnsetBypass()     { aetherBypass-- }
func aetherIsBypassed() bool { return aetherBypass > 0 }
