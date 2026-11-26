// The body of sha3Unwrap, which gc links to crypto/sha3's
// fips140hash_sha3Unwrap (v2 evaluation). That function returns &sha3.s,
// the Digest that is the only field of crypto/sha3's SHA3 (Go 1.26), so at
// its start: the same pointer.

package fips140hash

import (
	fsha3 "crypto/internal/fips140/sha3"
	"crypto/sha3"
	"unsafe"
)

func aetherSha3Unwrap(s *sha3.SHA3) *fsha3.Digest { return (*fsha3.Digest)(unsafe.Pointer(s)) }
