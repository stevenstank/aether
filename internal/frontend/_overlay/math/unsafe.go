// Aether's replacement of math/unsafe.go (D-049): intrinsics, whose bodies
// lowering generates, instead of conversions through unsafe.Pointer.

package math

// Float32bits returns the IEEE 754 binary representation of f.
func Float32bits(f float32) uint32

// Float32frombits returns the floating-point number of the IEEE 754
// binary representation b.
func Float32frombits(b uint32) float32

// Float64bits returns the IEEE 754 binary representation of f.
func Float64bits(f float64) uint64

// Float64frombits returns the floating-point number of the IEEE 754
// binary representation b.
func Float64frombits(b uint64) float64
