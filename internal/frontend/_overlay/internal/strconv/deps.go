// Aether's replacement of internal/strconv/deps.go (D-049): intrinsics,
// whose bodies lowering generates, instead of conversions through
// unsafe.Pointer.

package strconv

func float64frombits(b uint64) float64
func float32frombits(b uint32) float32
func float64bits(f float64) uint64
func float32bits(f float32) uint32

// inf, isNaN, and nan are those of GOROOT's deps.go.

func inf(sign int) float64 {
	if sign >= 0 {
		return float64frombits(0x7FF0000000000000)
	}
	return float64frombits(0xFFF0000000000000)
}

func isNaN(f float64) (is bool) { return f != f }

func nan() float64 { return float64frombits(0x7FF8000000000001) }
