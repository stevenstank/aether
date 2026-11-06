package main

import (
	"fmt"
	"io"
	"maps"
	"math/big"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// selectType is an integer type the differential programs are built for.
type selectType struct {
	name   string
	width  int
	signed bool
}

var selectTypes = []selectType{{"int", 64, true}, {"int8", 8, true}, {"int16", 16, true}, {"int32", 32, true}, {"int64", 64, true},
	{"uint", 64, false}, {"uint8", 8, false}, {"uint16", 16, false}, {"uint32", 32, false}, {"uint64", 64, false}}

// selectValues are the values of type t the programs use: around the edges of
// every type, of the 32-bit immediates, and of the shorter load forms.
func selectValues(t selectType) []string {
	one := big.NewInt(1)
	var vals []string
	add := func(v *big.Int) {
		lo, hi := big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(t.width)), one)
		if t.signed {
			lo = new(big.Int).Neg(new(big.Int).Lsh(one, uint(t.width-1)))
			hi = new(big.Int).Sub(new(big.Int).Lsh(one, uint(t.width-1)), one)
		}
		if v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0 && !slices.Contains(vals, v.String()) {
			vals = append(vals, v.String())
		}
	}
	for _, v := range []int64{0, 1, -1, 2, 7, 8, 64, 127, 128, -128, -129, 255} {
		add(big.NewInt(v))
	}
	for _, e := range []uint{15, 16, 31, 32, 63, 64} {
		p := new(big.Int).Lsh(one, e)
		add(p)
		add(new(big.Int).Sub(p, one))
		add(new(big.Int).Neg(p))
		add(new(big.Int).Sub(new(big.Int).Neg(p), one))
	}
	return vals
}

// selectProgram is a program that runs every form selection has, for type t,
// on every pair of selectValues. Results go into a hash printed once per
// call, which keeps the many values from turning into many write calls.
func selectProgram(t selectType) []byte {
	var b strings.Builder
	vals := selectValues(t)
	b.WriteString(`package main

var h uint64

func m(v uint64) { h = h*1000003 + v }

func mb(v bool) {
	if v {
		m(1)
	} else {
		m(2)
	}
}

func fence() {}
`)
	fmt.Fprintf(&b, "\nvar g %s\n", t.name)
	ops := []string{"==", "!=", "<", "<=", ">", ">="}
	for i, k := range vals {
		k := "(" + k + ")"
		fmt.Fprintf(&b, "\nfunc f%d(x, y %s) {\n", i, t.name)
		for _, op := range ops {
			for _, c := range []string{"x " + op + " " + k, k + " " + op + " x", "!(x " + op + " " + k + ")", "x " + op + " y"} {
				fmt.Fprintf(&b, "\tif %s {\n\t\tm(1)\n\t} else {\n\t\tm(2)\n\t}\n\tmb(%s)\n", c, c)
			}
			// A comparison used twice; one with its operands and result in
			// cells, since they are live across calls.
			fmt.Fprintf(&b, "\t{\n\t\tc := x %s %s\n\t\tif c {\n\t\t\tm(3)\n\t\t}\n\t\tif !c {\n\t\t\tm(4)\n\t\t}\n\t}\n", op, k)
			fmt.Fprintf(&b, "\t{\n\t\tp, q := x+1, y+1\n\t\tc := p %s q\n\t\td := p %s %s\n\t\tfence()\n\t\tif p %s %s {\n\t\t\tm(5)\n\t\t}\n\t\tif p %s q {\n\t\t\tm(6)\n\t\t}\n\t\tif c {\n\t\t\tm(7)\n\t\t}\n\t\tmb(d)\n\t}\n", op, op, k, op, k, op)
		}
		exprs := []string{"x + K", "x - K", "K - x", "K + x", "x * K", "K * x", "x & K", "K & x", "x | K", "K | x", "x ^ K", "K ^ x", "x &^ K", "K &^ x"}
		if k != "(0)" {
			exprs = append(exprs, "x / K", "x % K")
		}
		for _, e := range exprs {
			e = strings.ReplaceAll(e, "K", k)
			fmt.Fprintf(&b, "\tm(uint64(%s))\n", e)
			// The result in a cell.
			fmt.Fprintf(&b, "\t{\n\t\tr := %s\n\t\tfence()\n\t\tm(uint64(r))\n\t}\n", e)
		}
		// A constant dividend, the variable divisor checked.
		fmt.Fprintf(&b, "\tif x != 0 {\n\t\tm(uint64(%s / x))\n\t\tm(uint64(%s %% x))\n\t}\n", k, k)
		// Constants as phi operands, on edges with and without other
		// copies, as arguments, in a global, and as a result.
		fmt.Fprintf(&b, "\tv := %s(%s)\n\tif x < y {\n\t\tv = x\n\t}\n\tm(uint64(v))\n", t.name, k)
		fmt.Fprintf(&b, "\ta, c := x, %s(%s)\n\tfor i := 0; i < 3; i++ {\n\t\tif a == c {\n\t\t\tbreak\n\t\t}\n\t\ta, c = c, a\n\t}\n\tm(uint64(a))\n\tm(uint64(c))\n", t.name, k)
		fmt.Fprintf(&b, "\tg = %s\n\tm(uint64(g))\n\tm(uint64(r%d()))\n", k, i)
		fmt.Fprintf(&b, "\tm(l%d(x, y))\n\tm(uint64(a%d(x)))\n\tm(uint64(s%d(x)))\n\tm(uint64(p%d(x)))\n", i, i, i, i)
		fmt.Fprintf(&b, "}\n\nfunc r%d() %s { return %s }\n", i, t.name, k)
		// A leaf keeps its values in registers, unlike f, where they are live
		// across calls. x stays live after each operation, so the result goes
		// to a register of its own; in a%d, s%d, and p%d x dies in the operation.
		fmt.Fprintf(&b, "\nfunc a%d(x %s) %s { return x + %s }\n\nfunc s%d(x %s) %s { return x - %s }\n\nfunc p%d(x %s) %s { return x * %s }\n",
			i, t.name, t.name, k, i, t.name, t.name, k, i, t.name, t.name, k)
		fmt.Fprintf(&b, "\nfunc l%d(x, y %s) uint64 {\n\tr := uint64(0)\n", i, t.name)
		for _, e := range exprs {
			fmt.Fprintf(&b, "\tr = r*31 + uint64(%s) + uint64(x)\n", strings.ReplaceAll(e, "K", k))
		}
		for _, op := range ops {
			for _, c := range []string{"x " + op + " " + k, k + " " + op + " x", "!(x " + op + " " + k + ")", "x " + op + " y"} {
				fmt.Fprintf(&b, "\tif %s {\n\t\tr = r*31 + 1\n\t} else {\n\t\tr = r*31 + 2\n\t}\n", c)
			}
			fmt.Fprintf(&b, "\t{\n\t\tc := x %s %s\n\t\tif c {\n\t\t\tr += 3\n\t\t}\n\t\tif !c {\n\t\t\tr += 5\n\t\t}\n\t}\n", op, k)
		}
		fmt.Fprintf(&b, "\tv := %s(%s)\n\tif x < y {\n\t\tv = x\n\t}\n\tr = r*31 + uint64(v)\n", t.name, k)
		fmt.Fprintf(&b, "\ta, c := x, %s(%s)\n\tfor i := 0; i < 3; i++ {\n\t\tif a == c {\n\t\t\tbreak\n\t\t}\n\t\ta, c = c, a\n\t}\n\treturn r*31 + uint64(a) + uint64(c)\n}\n", t.name, k)
	}
	fmt.Fprintf(&b, "\nfunc shifts(x %s, n uint) {\n", t.name)
	for _, c := range []int{0, 1, t.width - 1, t.width, t.width + 1, 63, 64, 65, 1000} {
		fmt.Fprintf(&b, "\tm(uint64(x << %d))\n\tm(uint64(x >> %d))\n\t{\n\t\tr, s := x<<%d, x>>%d\n\t\tfence()\n\t\tm(uint64(r))\n\t\tm(uint64(s))\n\t}\n", c, c, c, c)
	}
	for _, k := range vals {
		fmt.Fprintf(&b, "\tm(uint64(%s(%s) << (n & 127)))\n\tm(uint64(%s(%s) >> (n & 127)))\n", t.name, k, t.name, k)
	}
	fmt.Fprintf(&b, "\tm(leafShifts(x))\n}\n\nfunc leafShifts(x %s) uint64 {\n\tr := uint64(0)\n", t.name)
	for _, c := range []int{0, 1, t.width - 1, t.width, t.width + 1, 63, 64, 65, 1000} {
		fmt.Fprintf(&b, "\tr = r*31 + uint64(x<<%d) + uint64(x>>%d) + uint64(x)\n", c, c)
	}
	b.WriteString("\treturn r\n}\n\nfunc main() {\n")
	for i := range vals {
		for _, v := range vals {
			fmt.Fprintf(&b, "\tf%d(%s, %s)\n\tprintln(%d, h)\n", i, v, vals[(i+3)%len(vals)], i)
		}
	}
	for i, v := range vals {
		fmt.Fprintf(&b, "\tshifts(%s, %d)\n\tprintln(h)\n", v, i*7)
	}
	b.WriteString("\tprintln(h)\n}\n")
	return []byte(b.String())
}

// selectFixed are programs about the structure of the code around branches.
var selectFixed = map[string]string{
	// Operands evaluated left to right around the comparison (D-024).
	"sel_order": `package main

var g int

func next() int {
	g++
	return g * 10
}

func check(x int) bool {
	println(x)
	return x > 5
}

func main() {
	if g < next() {
		println(1, g)
	}
	if next() < g {
		println(2, g)
	}
	if next()-g > g-next() {
		println(3, g)
	}
	if check(1) && check(9) || check(2) {
		println(4)
	}
	if !check(1) || !check(9) {
		println(5)
	}
	b := g == 3
	if b != (g < 100) {
		println(6)
	}
}
`,
	// Blocks that do nothing, including empty infinite loops that never run,
	// and branches whose two targets end up the same.
	"sel_empty": `package main

func f(x int) int {
	if x > 100 {
		for {
		}
	}
	if x > 90 {
		for {
			for {
			}
		}
	}
	if x > 3 {
	}
	if x > 2 {
	} else {
	}
	y := 1
	if x > 1 {
		if x > 2 {
			y = 2
		}
	} else if x > 0 {
		y = 3
	}
	switch {
	case x == 1:
	case x == 2:
		y += 10
	default:
	}
	return y
}

func main() {
	for i := 0; i < 6; i++ {
		println(f(i))
	}
	for true {
		break
	}
	if !false {
		println(true, !true)
	}
}
`,
	// Division by a constant that panics only on the path that divides.
	"sel_panic": `package main

func f(x, y int) int {
	return x<<1 + x/y
}

func main() {
	println(f(4, 2))
	println(f(1, 0))
}
`,
	// More live values than registers, with constants all around them.
	"sel_pressure": `package main

func f(a, b, c, d, e, g, h, i, j, k, l, m, n, o int) int {
	s := 0
	for t := 0; t < 4; t++ {
		a, b, c = b+1, c*3, a^5
		d, e, g = e-7, g&^8, d|9
		h, i, j = i<<2, j>>1, h%5
		k, l, m = l+k, m-l, k*m
		if a < 10 && b > 20 || c == 30 {
			s += a + b + c
		}
		if n > o {
			n, o = o, n
		}
		s += d + e + g + h + i + j + k + l + m + n + o
	}
	return s
}

func main() {
	println(f(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14))
	println(f(-1, 200, 30, -4000, 5, 60000, -7, 8, 900, 10, 1100000, -12, 13, 5000000000))
}
`,
}

// TestSelectDifferential runs the programs of selectProgram and selectFixed
// built by gc, by aether build, and with -O0, and in the interpreter: all
// four must agree.
func TestSelectDifferential(t *testing.T) {
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	progs := map[string][]byte{}
	for _, ty := range selectTypes {
		progs["sel_"+ty.name] = selectProgram(ty)
	}
	for name, src := range selectFixed {
		progs[name] = []byte(src)
	}
	want := gcRun(t, gobin, progs)
	for name, r := range want {
		if q, ok, err := panicPrefix(r); err != nil {
			t.Fatalf("%s: gc output does not fit the panic rule: %v", name, err)
		} else if ok {
			want[name] = result{stderr: q, exit: 2}
		}
	}
	dir := t.TempDir()
	for _, name := range slices.Sorted(maps.Keys(progs)) {
		got, ds, err := interpret(name+".go", progs[name])
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", name, ds, err)
		}
		if got != want[name] {
			t.Errorf("%s: interpreter differs from gc:\n%s", name, diffResults(got, want[name]))
		}
		for _, o0 := range []bool{false, true} {
			exe := filepath.Join(dir, fmt.Sprintf("%s.%v", name, o0))
			ds, err := build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe, o0: o0}, io.Discard)
			if len(ds) > 0 || err != nil {
				t.Fatalf("%s: -O0 %v: not built: %v %v", name, o0, ds, err)
			}
			if got := runExe(t, exe); got != want[name] {
				t.Errorf("%s: -O0 %v: native differs from gc:\n%s", name, o0, diffResults(got, want[name]))
			}
		}
	}
}

// diffResults describes the first difference of two results.
func diffResults(got, want result) string {
	if got.exit != want.exit {
		return fmt.Sprintf("exit %d, want %d", got.exit, want.exit)
	}
	g, w := strings.Split(got.stderr, "\n"), strings.Split(want.stderr, "\n")
	for i := range min(len(g), len(w)) {
		if g[i] != w[i] {
			return fmt.Sprintf("line %d: %q, want %q", i+1, g[i], w[i])
		}
	}
	return fmt.Sprintf("%d lines, want %d", len(g), len(w))
}

// TestSelectShapes checks the code selected for f: each must regexp must
// match it and no mustNot.
func TestSelectShapes(t *testing.T) {
	const r = `%r[a-z0-9]+`
	cases := []struct {
		name, body    string
		must, mustNot []string
	}{
		{"compare and branch", "if x < 10 { println(1) }",
			[]string{`cmpq\t\$10, ` + r + `\n\tjge\t`}, []string{`setl`, `\$10, %r[a-z0-9]+\n\tmovq`}},
		{"immediate on the left", "if 10 < x { println(1) }",
			[]string{`cmpq\t\$10, ` + r + `\n\tjle\t`}, []string{`set`}},
		{"zero", "if x == 0 { println(1) }", []string{`testq\t(` + r + `), (` + r + `)\n\tjne\t`}, []string{`cmpq\t\$`, `sete`}},
		{"unsigned", "if uint(x) < 10 { println(1) }", []string{`\n\tjae\t`}, []string{`setb`}},
		{"not", "if !(x < y) { println(1) }", []string{`cmpq\t` + r + `, ` + r + `\n\tjl\t`}, []string{`xorq\t\$1`, `setl`}},
		{"not of a value", "b := x < y\nprintln(b)\nif !b { println(1) }", []string{`cmpq\t\$0, -\d+\(%rbp\)\n\tjne\t`}, []string{`xorq\t\$1`}},
		{"value kept", "c := x < y\nif c { println(1) }\nif c { println(2) }", []string{`setl\t%al\n\tmovzbl\t%al, %e`}, nil},
		{"add", "println(x + 1)", []string{`leaq\t1\(` + r + `\), ` + r}, []string{`\$1, %r`}},
		{"sub", "println(x - 1)", []string{`leaq\t-1\(` + r + `\), ` + r}, nil},
		{"sub of the minimum", "println(x - (-2147483648))", []string{`subq\t\$-2147483648, `}, []string{`leaq`}},
		{"just past an immediate", "println(x + 2147483648)", []string{`movl\t\$2147483648, %e`}, nil},
		{"mul", "println(x * 10)", []string{`imulq\t\$10, ` + r + `, ` + r}, nil},
		{"and-not", "println(x &^ 6)", []string{`andq\t\$-7, `}, []string{`notq`}},
		{"bitwise stays canonical", "println(int8(x) & 3)", []string{`andq\t\$3, ` + r + `\n`}, []string{`andq\t\$3, ` + r + `\n\tmovs`}},
		{"narrow add", "println(int8(x) + 3)", []string{`movsbq\t%[a-z0-9]+, ` + r}, nil},
		{"shift", "println(x << 3, x >> 3, uint(x) >> 3)", []string{`shlq\t\$3, `, `sarq\t\$3, `, `shrq\t\$3, `}, []string{`%cl`, `cmovae`}},
		{"shift past the width", "println(x << 64, x >> 64, uint(x) >> 64)", []string{`xorl\t`, `sarq\t\$63, `}, []string{`%cl`, `cmovae`}},
		{"divide", "println(x / 3)", []string{`idivq`}, []string{`cmpq\t\$-1`}},
		{"divide by -1", "println(x / -1)", []string{`negq`}, []string{`idivq`}},
		{"large constant", "z := y + 1\nw := 5000000000\nprintln(z + w)", []string{`movabsq\t\$5000000000, ` + r}, nil},
		{"zero constant", "z := 0\nif x > 0 { z = y }\nprintln(z + y)", []string{`xorl\t%e`}, []string{`movq\t\$0, %r`}},
		{"global", "g = 5", []string{`movq\t\$5, main\.g\(%rip\)`}, nil},
		{"result", "if x > 0 { return 7 }", []string{`movq\t\$7, \d+\(%rbp\)`}, nil},
		{"argument", "h(9)", []string{`movq\t\$9, 0\(%rsp\)`}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nvar g int\n\nfunc h(a int) {}\n\nfunc f(x, y int) int {\n" + c.body + "\nreturn 0\n}\n\nfunc main() { println(f(1, 2), g); h(0) }\n"
			prog, ds := compile([]source{{"p.go", []byte(src)}})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			asm, err := emitProgram(prog)
			if err != nil {
				t.Fatal(err)
			}
			body := string(asm)
			body = body[strings.Index(body, "\nmain.f:"):]
			body = body[:strings.Index(body, ".size\tmain.f")]
			// The prologue compares %rsp and is the same for every function.
			body = body[strings.Index(body, ".L"):]
			body = regexp.MustCompile(`(?m)^\t#.*\n`).ReplaceAllString(body, "")
			for _, m := range c.must {
				if !regexp.MustCompile(m).MatchString(body) {
					t.Errorf("no match for %q in:\n%s", m, body)
				}
			}
			for _, m := range c.mustNot {
				if regexp.MustCompile(m).MatchString(body) {
					t.Errorf("match for %q in:\n%s", m, body)
				}
			}
		})
	}
}

// TestSelectNoLarger checks that selection never makes a function longer or
// its frame larger than registers alone, on the S2 suite and the workloads.
func TestSelectNoLarger(t *testing.T) {
	progs, _ := s2Suite(t)
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	var before, after codeStats
	for _, p := range progs {
		sel, sStack := measureAsm(t, emitPromoted(t, p.name, p.src, amd64.EmitOptions{Registers: true, Select: true}))
		reg, rStack := measureAsm(t, emitRegisters(t, p.name, p.src))
		before.mainStack += rStack
		after.mainStack += sStack
		for _, fn := range slices.Sorted(maps.Keys(reg)) {
			before.add(reg[fn])
			after.add(sel[fn])
			if sel[fn].instrs > reg[fn].instrs || sel[fn].frame > reg[fn].frame {
				t.Errorf("%s: %s: %d instructions and frame %d with selection, %d and %d without",
					p.name, fn, sel[fn].instrs, sel[fn].frame, reg[fn].instrs, reg[fn].frame)
			}
		}
	}
	t.Logf("machine instructions: registers %d, with selection %d (%s)", before.instrs, after.instrs, ratio(after.instrs, before.instrs))
	t.Logf("frame bytes: registers %d, with selection %d (%s)", before.frame, after.frame, ratio(after.frame, before.frame))
}
