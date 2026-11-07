package amd64

// Register forms of arithmetic for aether build: EmitOptions.TwoAddress.
//
// The accumulator sequences of instr compute through %rax and then copy the
// result to its location: x86's two-operand instructions overwrite their
// destination, so x op y in register t is "movq x, %rax; op y, %rax; movq %rax,
// t". When t is a register, twoAddress computes in t itself: "movq x, t; op y,
// t", with no move when x is already in t. If t holds y (y dies here and the
// allocator reused its register), the move would destroy y, so a commutative
// operation swaps its operands, x - y becomes -y + x, and x &^ y becomes ^y & x.
// x + y with both operands in other registers is a single leaq. A conversion
// that does not change the value's bits is a move, and any other one extends
// straight from the source register.
//
// Canonical form (D-023) is restored only where the result can leave it: Add,
// Sub, Mul, Neg, the complement of an unsigned value, and a conversion that
// changes the bits. The bitwise operations of canonical values are canonical,
// as selectBinary explains.
//
// checkArith verifies every such sequence independently of how it was chosen,
// by running its text on concrete operand values (arithSim) and comparing the
// result with the IR's definition. It also checks that the sequence writes
// nothing but its destination and the scratch %rcx. Running on fixed vectors is
// a check, not a proof; what it catches is an operand overwritten before it is
// read, a wrong instruction, and a missing or wrong canonicalization.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stevenstank/aether/internal/ir"
)

// arithSite is the text of one register-form sequence: e.b's bytes start to
// end.
type arithSite struct {
	in         *ir.Instr
	start, end int
}

// preserves reports whether converting a canonical value of type from to type
// to leaves its 64 bits unchanged: to is 64 bits wide, or wider than from and
// signed if from is.
func preserves(from, to ir.Type) bool {
	return to.Width() == 64 || from == to || from.Width() < to.Width() && (!from.Signed() || to.Signed())
}

// canonFrom puts the canonical form for t, narrower than 64 bits, of the
// register src into the register dst.
func (e *emitter) canonFrom(src, dst string, t ir.Type) {
	s, d := subRegs[src], subRegs[dst]
	switch t {
	case ir.I8:
		e.op("movsbq\t%s, %s", s[2], dst)
	case ir.I16:
		e.op("movswq\t%s, %s", s[1], dst)
	case ir.I32:
		e.op("movslq\t%s, %s", s[0], dst)
	case ir.U8:
		e.op("movzbl\t%s, %s", s[2], d[0])
	case ir.U16:
		e.op("movzwl\t%s, %s", s[1], d[0])
	case ir.U32:
		e.op("movl\t%s, %s", s[0], d[0])
	default:
		e.fail(fmt.Errorf("no canonical form for %s", t))
	}
}

// twoAddress emits in, an Add, Sub, Mul, And, Or, Xor, AndNot, Neg, Com, or
// Conv, in its result's register, and reports whether it did: not if the
// option is off or the result has a cell.
func (e *emitter) twoAddress(in *ir.Instr) bool {
	t := e.val(in)
	if !e.sel.twoAddr || !isReg(t) {
		return false
	}
	start := e.b.Len()
	switch in.Op {
	case ir.Neg, ir.Com:
		e.load(in.Args[0], t)
		if in.Op == ir.Neg {
			e.op("negq\t%s", t)
		} else {
			e.op("notq\t%s", t)
		}
		if in.Op == ir.Neg || !in.Type.Signed() {
			e.canonReg(t, in.Type)
		}
	case ir.Conv:
		x := in.Args[0]
		xs := e.val(x)
		switch {
		case preserves(x.Type, in.Type):
			e.move(xs, t)
		case isReg(xs):
			e.canonFrom(xs, t, in.Type)
		default:
			e.move(xs, t)
			e.canonReg(t, in.Type)
		}
	default:
		e.binaryInPlace(in, t)
	}
	e.arith = append(e.arith, arithSite{in, start, e.b.Len()})
	return true
}

// binaryInPlace emits the binary operation in in the register t.
func (e *emitter) binaryInPlace(in *ir.Instr, t string) {
	xs, ys := e.val(in.Args[0]), e.val(in.Args[1])
	switch {
	case in.Op == ir.Add && isReg(xs) && isReg(ys) && xs != t && ys != t:
		e.op("leaq\t(%s,%s), %s", xs, ys, t)
	case in.Op == ir.Sub && ys == t && xs != t:
		e.op("negq\t%s", t)
		e.op("addq\t%s, %s", xs, t)
	case in.Op == ir.AndNot && ys == t && xs != t:
		e.op("notq\t%s", t)
		e.op("andq\t%s, %s", xs, t)
	case in.Op == ir.AndNot && xs == t:
		e.move(ys, "%rcx")
		e.op("notq\t%%rcx")
		e.op("andq\t%%rcx, %s", t)
	case in.Op == ir.AndNot:
		// t holds neither operand.
		e.move(ys, t)
		e.op("notq\t%s", t)
		e.op("andq\t%s, %s", xs, t)
	default:
		if ys == t && xs != t {
			// Commutative, since Sub and AndNot were handled above.
			xs, ys = ys, xs
		}
		// Now t holds x or neither operand, so the move keeps y.
		e.move(xs, t)
		mnemonic := map[ir.Op]string{ir.Add: "addq", ir.Sub: "subq", ir.Mul: "imulq", ir.And: "andq", ir.Or: "orq", ir.Xor: "xorq"}[in.Op]
		e.op("%s\t%s, %s", mnemonic, ys, t)
	}
	if in.Op == ir.Add || in.Op == ir.Sub || in.Op == ir.Mul {
		e.canonReg(t, in.Type)
	}
}

// arithVectors are the operand values checkArith runs each sequence on, each
// reduced to its operand's type. Within a pair the values differ at every
// width, so a sequence that reads an overwritten operand gets a wrong result.
// The first two pairs overflow a signed sum at every width, upward and
// downward, and the next two an unsigned sum and difference. Each width's
// signed minimum is the first operand of a pair, for negation and difference.
var arithVectors = [][2]uint64{
	{0x7fffffff7fff7f7f, 0x0000000100010101},
	{0x8000000080008080, 0xffffffffffffffff},
	{0xffffffffffffffff, 0x0000000200020202},
	{1, 2},
	{0x80000000, 0x10001},
	{0x8000, 0x101},
	{0xffffffffffffffff, 3},
	{0x8000000000000000, 0x7fffffffffffffff},
	{0x7fffffffffffffff, 0xffffffffffffffff},
	{0x123456789abcdef0, 0xfedcba9876543211},
	{0x80, 0x7f},
	{0xffff8000, 0x7fffffff},
	{0, 0x5555555555555555},
}

// arithEval is the IR's definition of in on canonical operands.
func arithEval(in *ir.Instr, x, y int64) int64 {
	var v int64
	switch in.Op {
	case ir.Add:
		v = x + y
	case ir.Sub:
		v = x - y
	case ir.Mul:
		v = x * y
	case ir.And:
		v = x & y
	case ir.Or:
		v = x | y
	case ir.Xor:
		v = x ^ y
	case ir.AndNot:
		v = x &^ y
	case ir.Neg:
		v = -x
	case ir.Com:
		v = ^x
	case ir.Conv:
		v = x
	}
	return in.Type.Canonical(v)
}

// checkArith verifies the register-form sequences e emitted for its function.
func (e *emitter) checkArith() error {
	text := e.b.Bytes()
	for _, s := range e.arith {
		code := parseInstrs(text[s.start:s.end])
		dst := e.val(s.in)
		for _, vec := range arithVectors {
			sim := arithSim{held: map[string]uint64{}, may: map[string]bool{dst: true, "%rcx": true}}
			var vals [2]int64
			for i, a := range s.in.Args {
				loc := e.val(a)
				if isImm(loc) {
					c, err := strconv.ParseInt(loc[1:], 10, 64)
					if err != nil {
						return fmt.Errorf("%s: bad immediate %s", s.in, loc)
					}
					vals[i] = c
					continue
				}
				if _, ok := sim.held[loc]; !ok {
					sim.held[loc] = uint64(a.Type.Canonical(int64(vec[i])))
				}
				vals[i] = int64(sim.held[loc])
			}
			if err := sim.run(code); err != nil {
				return fmt.Errorf("%s: %v", s.in, err)
			}
			if got, want := int64(sim.held[dst]), arithEval(s.in, vals[0], vals[1]); got != want {
				return fmt.Errorf("%s: computes %d, not %d, from %v", s.in, got, want, vals[:len(s.in.Args)])
			}
		}
	}
	return nil
}

// arithSim runs the instructions of register-form sequences on concrete
// values. Locations are named by their operand text; a register's narrower
// names read and write it.
type arithSim struct {
	held map[string]uint64
	may  map[string]bool // the locations the sequence may write
}

// reg64 returns the 64-bit register a register name is part of and the width
// of the name, or "" if it names no register of subRegs.
func reg64(name string) (string, int) {
	if _, ok := subRegs[name]; ok {
		return name, 64
	}
	for r, sub := range subRegs {
		for i, s := range sub {
			if s == name {
				return r, [3]int{32, 16, 8}[i]
			}
		}
	}
	return "", 0
}

func (s *arithSim) read(op string) (uint64, error) {
	if isImm(op) {
		c, err := strconv.ParseInt(op[1:], 10, 64)
		return uint64(c), err
	}
	if r, w := reg64(op); r != "" {
		v, ok := s.held[r]
		if !ok {
			return 0, fmt.Errorf("reads %s, which holds no operand", op)
		}
		if w < 64 {
			v &= 1<<w - 1
		}
		return v, nil
	}
	v, ok := s.held[op]
	if !ok {
		return 0, fmt.Errorf("reads %s, which holds no operand", op)
	}
	return v, nil
}

// write sets the register or location op; a 32-bit register write zeroes the
// upper half, as x86-64 does. Narrower writes are not used.
func (s *arithSim) write(op string, v uint64) error {
	loc := op
	if r, w := reg64(op); r != "" {
		switch w {
		case 64:
		case 32:
			v &= 0xffffffff
		default:
			return fmt.Errorf("unexpected write of %s", op)
		}
		loc = r
	}
	if !s.may[loc] {
		return fmt.Errorf("writes %s", loc)
	}
	s.held[loc] = v
	return nil
}

func (s *arithSim) run(code []asmInstr) error {
	for _, in := range code {
		if in.op == "leaq" {
			base, index, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(in.src, "("), ")"), ",")
			if !ok || !strings.HasPrefix(in.src, "(") {
				return fmt.Errorf("unexpected leaq %s", in.src)
			}
			x, err := s.read(base)
			if err != nil {
				return err
			}
			y, err := s.read(index)
			if err != nil {
				return err
			}
			if err := s.write(in.dst, x+y); err != nil {
				return err
			}
			continue
		}
		if in.dst == "" {
			// negq and notq: src is the operand, read and written.
			v, err := s.read(in.src)
			if err != nil {
				return err
			}
			switch in.op {
			case "negq":
				v = -v
			case "notq":
				v = ^v
			default:
				return fmt.Errorf("unexpected %s %s", in.op, in.src)
			}
			if err := s.write(in.src, v); err != nil {
				return err
			}
			continue
		}
		if in.op == "xorl" {
			if in.src != in.dst {
				return fmt.Errorf("unexpected xorl %s, %s", in.src, in.dst)
			}
			if err := s.write(in.dst, 0); err != nil {
				return err
			}
			continue
		}
		x, err := s.read(in.src)
		if err != nil {
			return err
		}
		var v uint64
		switch in.op {
		case "movq", "movabsq", "movl", "movzbl", "movzwl":
			// The read already took the source's width.
			v = x
		case "movsbq":
			v = uint64(int64(int8(x)))
		case "movswq":
			v = uint64(int64(int16(x)))
		case "movslq":
			v = uint64(int64(int32(x)))
		default:
			y, err := s.read(in.dst)
			if err != nil {
				return err
			}
			switch in.op {
			case "addq":
				v = y + x
			case "subq":
				v = y - x
			case "imulq":
				v = y * x
			case "andq":
				v = y & x
			case "orq":
				v = y | x
			case "xorq":
				v = y ^ x
			default:
				return fmt.Errorf("unexpected %s %s, %s", in.op, in.src, in.dst)
			}
		}
		if err := s.write(in.dst, v); err != nil {
			return err
		}
	}
	return nil
}
