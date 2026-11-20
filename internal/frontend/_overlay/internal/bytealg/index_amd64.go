// Aether's replacement of internal/bytealg/index_amd64.go (D-049,
// increment 11.7), whose Index and IndexString are gc's assembly. The
// lengths only choose among algorithms with the same results.

package bytealg

// MaxBruteForce is the longest s that Index searches by brute force.
const MaxBruteForce = 64

func init() {
	MaxLen = 63
}

// Cutover returns the number of failures of IndexByte to tolerate, after
// n bytes, before switching to Index.
func Cutover(n int) int {
	return (n + 16) / 8
}

// Index returns the index of the first b in a, or -1.
func Index(a, b []byte) int {
	for i := 0; i+len(b) <= len(a); i++ {
		if same(a, i, b) {
			return i
		}
	}
	return -1
}

// IndexString returns the index of the first b in a, or -1.
func IndexString(a, b string) int {
	for i := 0; i+len(b) <= len(a); i++ {
		if same(a, i, b) {
			return i
		}
	}
	return -1
}
