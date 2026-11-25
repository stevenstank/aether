// Aether's replacement of math/log_asm.go (D-053): gc's amd64 assembly of
// Log ($GOROOT/src/math/log_amd64.s), written in Go instruction by
// instruction, so that its results are gc's bit for bit. Each operation is
// rounded on its own, as an explicit float64 conversion requires.

package math

const haveArchLog = true

// The assembly's constants, as its assembler rounds them.
const (
	logHSqrt2 = 7.07106781186547524401e-01 // sqrt(2)/2
	logLn2Hi  = 6.93147180369123816490e-01 // 0x3fe62e42fee00000
	logLn2Lo  = 1.90821492927058770002e-10 // 0x3dea39ef35793c76
	logL1     = 6.666666666666735130e-01   // 0x3FE5555555555593
	logL2     = 3.999999999940941908e-01   // 0x3FD999999997FA04
	logL3     = 2.857142874366239149e-01   // 0x3FD2492494229359
	logL4     = 2.222219843214978396e-01   // 0x3FCC71C51D8E78AF
	logL5     = 1.818357216161805012e-01   // 0x3FC7466496CB03DE
	logL6     = 1.531383769920937332e-01   // 0x3FC39A09D078C69F
	logL7     = 1.479819860511658591e-01   // 0x3FC2F112DF3E5244
	logNaN    = 0x7FF8000000000001
	logNegInf = 0xFFF0000000000000
	logPosInf = 0x7FF0000000000000
)

// archLog is gc's ·archLog of log_amd64.s. Like it, it takes a subnormal
// x's exponent field as it is (k = -1022) and its fraction without
// normalizing it, and returns the NaN 0x7FF8000000000001 for a negative x,
// a NaN with the sign bit included.
func archLog(x float64) float64 {
	bx := Float64bits(x)
	if bx&^(1<<63) == 0 { // ±0
		return Float64frombits(logNegInf)
	}
	if int64(bx) < 0 {
		return Float64frombits(logNaN)
	}
	if bx >= logPosInf {
		return x // +Inf or NaN
	}
	// f1, ki := math.Frexp(x); k := float64(ki)
	f1 := Float64frombits(bx&0x000FFFFFFFFFFFFF | 0x3FE0000000000000)
	k := float64(int32(bx>>52&0x7FF) - 0x3FE)
	// if f1 <= math.Sqrt2/2 { k -= 1; f1 *= 2 }: CMPSD's "not less than"
	var c float64
	if !(logHSqrt2 < f1) {
		c = 1
	}
	k = float64(k - c)
	c = float64(c + 1)
	f1 = float64(f1 * c)
	f := float64(f1 - 1)
	s := float64(f / float64(2+f))
	s2 := float64(s * s)
	s4 := float64(s2 * s2)
	// t1 := s2 * (L1 + s4*(L3+s4*(L5+s4*L7)))
	t := float64(logL7 * s4)
	t = float64(float64(t+logL5) * s4)
	t = float64(float64(t+logL3) * s4)
	t1 := float64(s2 * float64(t+logL1))
	// t2 := s4 * (L2 + s4*(L4+s4*L6))
	t = float64(logL6 * s4)
	t = float64(float64(t+logL4) * s4)
	t2 := float64(s4 * float64(t+logL2))
	R := float64(t1 + t2)
	hfsq := float64(float64(0.5*f) * f)
	// return k*Ln2Hi - ((hfsq - (s*(hfsq+R) + k*Ln2Lo)) - f)
	u := float64(s * float64(R+hfsq))
	u = float64(u + float64(logLn2Lo*k))
	v := float64(float64(hfsq-u) - f)
	return float64(float64(k*logLn2Hi) - v)
}
