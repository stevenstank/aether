// Aether's replacement of internal/bytealg/bytealg.go (D-049, increment
// 11.7), whose offsets are for gc's assembly and whose MakeNoZero is its
// runtime's.

package bytealg

// MaxLen is the longest b that Index and IndexString search for.
var MaxLen int

// PrimeRK is the base of the Rabin-Karp hashes.
const PrimeRK = 16777619

// pow returns PrimeRK to the power n, modulo 2^32.
func pow(n int) uint32 {
	var p, sq uint32 = 1, PrimeRK
	for ; n > 0; n >>= 1 {
		if n&1 != 0 {
			p *= sq
		}
		sq *= sq
	}
	return p
}

// HashStr returns the Rabin-Karp hash of sep, and PrimeRK to the power
// len(sep).
func HashStr[T string | []byte](sep T) (uint32, uint32) {
	var h uint32
	for i := 0; i < len(sep); i++ {
		h = h*PrimeRK + uint32(sep[i])
	}
	return h, pow(len(sep))
}

// HashStrRev is HashStr of the reverse of sep.
func HashStrRev[T string | []byte](sep T) (uint32, uint32) {
	var h uint32
	for i := len(sep) - 1; i >= 0; i-- {
		h = h*PrimeRK + uint32(sep[i])
	}
	return h, pow(len(sep))
}

// same reports whether s[i:i+len(sep)] equals sep.
func same[T string | []byte](s T, i int, sep T) bool {
	for j := 0; j < len(sep); j++ {
		if s[i+j] != sep[j] {
			return false
		}
	}
	return true
}

// IndexRabinKarp returns the index of the first sep in s, or -1.
func IndexRabinKarp[T string | []byte](s, sep T) int {
	n := len(sep)
	h, p := HashStr(sep)
	var hs uint32
	for i := 0; i < n && i < len(s); i++ {
		hs = hs*PrimeRK + uint32(s[i])
	}
	for i := 0; i+n <= len(s); i++ {
		if hs == h && same(s, i, sep) {
			return i
		}
		if i+n < len(s) {
			hs = hs*PrimeRK + uint32(s[i+n]) - p*uint32(s[i])
		}
	}
	return -1
}

// LastIndexRabinKarp returns the index of the last sep in s, or -1.
func LastIndexRabinKarp[T string | []byte](s, sep T) int {
	n := len(sep)
	h, p := HashStrRev(sep)
	var hs uint32
	for i := len(s) - 1; i >= len(s)-n && i >= 0; i-- {
		hs = hs*PrimeRK + uint32(s[i])
	}
	for i := len(s) - n; i >= 0; i-- {
		if hs == h && same(s, i, sep) {
			return i
		}
		if i > 0 {
			hs = hs*PrimeRK + uint32(s[i-1]) - p*uint32(s[i-1+n])
		}
	}
	return -1
}

// MakeNoZero returns a slice of length and capacity n. Aether's bytes
// are zero.
func MakeNoZero(n int) []byte {
	return make([]byte, n)
}
