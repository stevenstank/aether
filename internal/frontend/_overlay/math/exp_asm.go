// Aether's replacement of math/exp_asm.go and exp_amd64.go (D-053): gc's
// amd64 assembly of Exp ($GOROOT/src/math/exp_amd64.s), written in Go
// instruction by instruction, so that its results are gc's bit for bit:
// the same operations on the same values in the same order, each rounded
// as the instruction rounds it. An fused multiply-add of the assembly's
// FMA path is FMA, exact as VFMADD is; each operation of the other path is
// rounded on its own, which an explicit float64 conversion requires
// (spec §Floating-point operators).

package math

const haveArchExp = true

// The assembly's constants, as its assembler rounds them.
const (
	expLN2U     = 0.69314718055966295651160180568695068359375 // upper half LN2
	expLN2L     = 0.28235290563031577122588448175013436025525412068e-12
	expLOG2E    = 1.4426950408889634073599246810018920
	expOverflow = 7.09782712893384e+02
	expPosInf   = 0x7FF0000000000000
	expNegInf   = 0xFFF0000000000000

	// exprodata, by offset.
	expC0  = 0.5
	expC8  = 1.0
	expC16 = 2.0
	expC24 = 1.6666666666666666667e-1
	expC32 = 4.1666666666666666667e-2
	expC40 = 8.3333333333333333333e-3
	expC48 = 1.3888888888888888889e-3
	expC56 = 1.9841269841269841270e-4
	expC64 = 2.4801587301587301587e-5
)

// useFMA is gc's (exp_amd64.go): cpu.X86.HasAVX && cpu.X86.HasFMA, which
// gc's runtime sets at startup from CPUID and XGETBV and then from the
// cpu options of GODEBUG. Aether sets it at the first Exp (fmaKnown), with
// the same value, so that a program not calling Exp does not compute it.
var useFMA, fmaKnown bool

// cpuFMA is 1 if the processor and the kernel provide AVX and FMA, else 0:
// an intrinsic (ir.SysCPUFMA).
func cpuFMA() int

// envc and env are the program's environment strings, as os's: intrinsics.
func envc() int
func env(i int) string

// godebugCPU reports whether the cpu options of GODEBUG leave both AVX and
// FMA on, as gc's internal/cpu.processOptions applies them: the first
// GODEBUG in the environment, as gc's runtime reads it, and in it each
// "cpu.avx", "cpu.fma", or "cpu.all" set to "on" or "off", the last one
// of each counting. (gc also prints warnings for malformed options,
// which Aether does not.)
func godebugCPU() bool {
	const prefix = "GODEBUG="
	var d string
	for i := 0; i < envc(); i++ {
		if s := env(i); len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			d = s[len(prefix):]
			break
		}
	}
	avx, fma := true, true
	for d != "" {
		field := d
		d = ""
		for i := 0; i < len(field); i++ {
			if field[i] == ',' {
				field, d = field[:i], field[i+1:]
				break
			}
		}
		if len(field) < 4 || field[:4] != "cpu." {
			continue
		}
		key, value := field[4:], ""
		for i := 0; i < len(key); i++ {
			if key[i] == '=' {
				key, value = key[:i], key[i+1:]
				break
			}
		}
		if value != "on" && value != "off" {
			continue
		}
		on := value == "on"
		switch key {
		case "all":
			avx, fma = on, on
		case "avx":
			avx = on
		case "fma":
			fma = on
		}
	}
	return avx && fma
}

// cvtsd2sl is CVTSD2SL: t rounded to the nearest integer, ties to even (the
// default rounding mode), or -1<<31 if that is not an int32 or t is NaN.
func cvtsd2sl(t float64) int32 {
	r := RoundToEven(t)
	if !(r >= -(1<<31) && r < 1<<31) {
		return -1 << 31
	}
	return int32(r)
}

// archExp is gc's ·archExp of exp_amd64.s.
func archExp(x float64) float64 {
	bx := Float64bits(x)
	if bx&^(1<<63) >= expPosInf { // not finite
		if bx == expNegInf {
			return 0
		}
		return x // NaN or +Inf
	}
	if x > expOverflow {
		return Float64frombits(expPosInf)
	}
	if !fmaKnown {
		useFMA, fmaKnown = cpuFMA() != 0 && godebugCPU(), true
	}
	k := cvtsd2sl(float64(expLOG2E * x))
	e := float64(k)
	if useFMA {
		x = FMA(-e, expLN2U, x) // VFNMADD231SD
		x = FMA(-e, expLN2L, x)
		x = float64(x * 0.0625) // reduce argument
		// Taylor series evaluation
		p := FMA(x, expC64, expC56) // VFMADD213SD
		p = FMA(x, p, expC48)
		p = FMA(x, p, expC40)
		p = FMA(x, p, expC32)
		p = FMA(x, p, expC24)
		p = FMA(x, p, expC0)
		p = FMA(x, p, expC8)
		x = float64(x * p)
		p = float64(x + expC16) // VADDSD
		x = float64(x * p)
		p = float64(x + expC16)
		x = float64(x * p)
		p = float64(x + expC16)
		x = float64(x * p)
		p = float64(x + expC16)
		x = FMA(x, p, expC8)
	} else {
		x = float64(x - float64(e*expLN2U))
		x = float64(x - float64(e*expLN2L))
		x = float64(x * 0.0625) // reduce argument
		// Taylor series evaluation
		p := float64(expC64 * x)
		p = float64(float64(p+expC56) * x)
		p = float64(float64(p+expC48) * x)
		p = float64(float64(p+expC40) * x)
		p = float64(float64(p+expC32) * x)
		p = float64(float64(p+expC24) * x)
		p = float64(float64(p+expC0) * x)
		p = float64(p + expC8)
		x = float64(x * p)
		p = float64(expC16 + x)
		x = float64(x * p)
		p = float64(expC16 + x)
		x = float64(x * p)
		p = float64(expC16 + x)
		x = float64(x * p)
		p = float64(expC16 + x)
		x = float64(x * p)
		x = float64(x + expC8)
	}
	// return fr * 2**exponent
	b := k + 0x3FF // add bias
	if b <= 0 {    // denormal
		if b < -52 {
			return 0
		}
		b += 0x3FE // add bias - 1
		x = float64(x * Float64frombits(uint64(uint32(b))<<52))
		b = 1
	} else if b >= 0x7FF {
		return Float64frombits(expPosInf)
	}
	return float64(x * Float64frombits(uint64(uint32(b))<<52))
}
