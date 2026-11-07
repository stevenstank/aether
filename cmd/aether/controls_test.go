package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// control is a negative control: a deliberate defect in one source file,
// which the named tests must detect. old must occur exactly once in file
// and is replaced by new.
type control struct {
	name     string
	file     string // relative to the module root
	old, new string
	pkg      string   // the package whose tests must fail
	run      string   // their -run pattern
	want     []string // regular expressions the failing output must match
	// interpOnly hides GNU as and ld, so the differential test compares
	// only the interpreter with gc (for a defect that makes native code
	// loop until its timeout).
	interpOnly bool
}

// controls are the Phase 4 negative controls, by increment. Each was first
// run by hand and reverted (ROADMAP.md Phase 4); here each is applied to a
// copy of the module, never to the working tree.
var controls = []control{
	// 4.1
	{name: "4.1 branch targets swapped", file: "internal/amd64/amd64.go",
		old: `op("jnz\t%s", label(in.Targets[0]))
			e.edgeCopies(els)
			op("jmp\t%s", label(in.Targets[1]))`,
		new: `op("jnz\t%s", label(in.Targets[1]))
			e.edgeCopies(els)
			op("jmp\t%s", label(in.Targets[0]))`,
		pkg: "./internal/amd64", run: "^TestNativeControlFlow$", want: []string{`--- FAIL: TestNativeControlFlow`}},
	{name: "4.1 setb for signed less", file: "internal/amd64/amd64.go",
		old: `ir.Lt: "setl"`, new: `ir.Lt: "setb"`,
		pkg: "./internal/amd64", run: "^TestNativeControlFlow$", want: []string{`--- FAIL: TestNativeControlFlow`}},
	{name: "4.1 printbool prints 1", file: "internal/amd64/rt/print.s",
		old: "aether.printbool:\n", new: "aether.printbool:\n\tjmp\taether.printint\n",
		pkg: "./internal/amd64", run: "^TestNativeControlFlow$", want: []string{`--- FAIL: TestNativeControlFlow`}},
	{name: "4.1 definite assignment on any path", file: "internal/ir/verify.go",
		old: `all := true
				for _, p := range preds[i] {
					all = all && (`,
		new: `all := false
				for _, p := range preds[i] {
					all = all || (`,
		pkg: "./internal/ir", run: "^TestVerifyControlFlow$", want: []string{`--- FAIL: TestVerifyControlFlow/store_on_one_path_only`, `--- FAIL: TestVerifyControlFlow/store_only_inside_the_loop_body`}},
	{name: "4.1 || lowered as &&", file: "internal/lower/lower.go",
		old: `if e.Op == token.LAND {`, new: `if e.Op == token.LAND || e.Op == token.LOR {`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`gen\d{4}: interpreter`, `gen\d{4}: native`}},
	{name: "4.1 continue skips the post statement", file: "internal/lower/lower.go",
		old: `lp := &target{next: next, exit: exit}`, new: `lp := &target{next: head, exit: exit}`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`: not run: \[\] interp: step budget exhausted`},
		interpOnly: true},

	// 4.2
	{name: "4.2 prologue without jb", file: "internal/amd64/amd64.go",
		old: `\tjb\t%s\n"`, new: `\t# jb\t%s\n"`,
		pkg: "./internal/amd64", run: "^(TestNativeStackOverflow|TestStackBoundary|TestFrameLargerThanStack)$",
		want: []string{`--- FAIL: TestNativeStackOverflow`, `--- FAIL: TestStackBoundary`, `--- FAIL: TestFrameLargerThanStack`, `exit -1`}},
	{name: "4.2 L never stored", file: "internal/amd64/rt/start.s",
		old: "\tmovq\t%rcx, aether.stacklimit(%rip)", new: "\t# movq\t%rcx, aether.stacklimit(%rip)",
		pkg: "./internal/amd64", run: "^(TestNativeStackOverflow|TestStackBoundary|TestFrameLargerThanStack)$",
		want: []string{`--- FAIL: TestNativeStackOverflow`, `--- FAIL: TestStackBoundary`, `--- FAIL: TestFrameLargerThanStack`, `exit -1`}},
	{name: "4.2 stack mapped with MAP_NORESERVE", file: "internal/amd64/rt/start.s",
		old: "$0x20022, %r10d", new: "$0x24022, %r10d",
		pkg: "./internal/amd64", run: "^TestStackMapping$", want: []string{`--- FAIL: TestStackMapping`, `nr`}},
	{name: "4.2 native overflow message changed", file: "internal/amd64/rt/start.s",
		old: `.ascii	"fatal error: stack overflow\n"`, new: `.ascii	"fatal error: stack overflow!\n"`,
		pkg: "./cmd/aether", run: "^TestStackOverflowAgainstGc$", want: []string{`: native `}},
	{name: "4.2 interpreter overflow message changed", file: "internal/interp/interp.go",
		old: `const stackOverflow = "fatal error: stack overflow\n"`, new: `const stackOverflow = "fatal error: stack overflow!\n"`,
		pkg: "./cmd/aether", run: "^TestStackOverflowAgainstGc$", want: []string{`: interpreter `}},

	// 4.3
	{name: "4.3 extract reads the outgoing area", file: "internal/amd64/amd64.go",
		old: `op("movq\t%d(%%rbp), %%rax", fr.ValueOffset(tuples[in.Args[0]]+in.Index))`,
		new: `op("movq\t%d(%%rsp), %%rax", 8*(len(in.Args[0].Args)+in.Index))`,
		pkg: "./internal/amd64", run: "^TestNativeTuples$", want: []string{`--- FAIL: TestNativeTuples`}},

	// 4.4
	{name: "4.4 no canonicalization", file: "internal/amd64/amd64.go",
		old: "\t\top(\"%s\", c)\n", new: "\t\t_ = c\n",
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`--- FAIL: TestIntegerTypes`}},
	{name: "4.4 signed condition codes for unsigned types", file: "internal/amd64/amd64.go",
		old: `cc = setccUnsigned[in.Op]`, new: `cc = setcc[in.Op]`,
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`--- FAIL: TestIntegerTypes`}},
	{name: "4.4 printint for unsigned values", file: "internal/amd64/amd64.go",
		old: `op("call\t%s", symPrintUint)`, new: `op("call\t%s", symPrintInt)`,
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`--- FAIL: TestIntegerTypes`}},
	{name: "4.4 interpreter compares unsigned values as signed", file: "internal/interp/interp.go",
		old: `x, y = x^math.MinInt64, y^math.MinInt64`, new: `_, _ = x^math.MinInt64, y^math.MinInt64`,
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`interpreter: exit 0, <nil>, output differs`}},
	{name: "4.4 conversion executed as a no-op", file: "internal/interp/interp.go",
		old: `fr.values[in.ID] = in.Type.Canonical(arg(0))`, new: `fr.values[in.ID] = arg(0)`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`gen\d{4}: interpreter`}},
	{name: "4.4 &^ lowered as &", file: "internal/lower/lower.go",
		old: `token.XOR: ir.Xor, token.AND_NOT: ir.AndNot,
	token.QUO`, new: `token.XOR: ir.Xor, token.AND_NOT: ir.And,
	token.QUO`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`gen\d{4}: interpreter`, `gen\d{4}: native`}},

	// 4.5
	{name: "4.5 idivq for every divisor", file: "internal/amd64/amd64.go",
		old: `op("cmpq\t$-1, %%rcx")
			op("jne\t1f")`, new: `op("jmp\t1f")`,
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`--- FAIL: TestIntegerTypes`}},
	{name: "4.5 no width check on shifts", file: "internal/amd64/amd64.go",
		old: `op("cmovaeq\t%%rdx, %%rax")`, new: ``,
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`--- FAIL: TestIntegerTypes`}},
	{name: "4.5 zero instead of the sign fill", file: "internal/amd64/amd64.go",
		old: `op("sarq\t$63, %%rdx")`, new: `op("xorl\t%%edx, %%edx")`,
		pkg: "./internal/amd64", run: "^TestIntegerTypes$", want: []string{`--- FAIL: TestIntegerTypes`}},
	{name: "4.5 interpreter divides unsigned values as signed", file: "internal/interp/interp.go",
		old: `if !t.Signed() {
		if op == ir.Div {`, new: `if !t.Signed() {
		w := 64 - t.Width()
		x, y = x<<w>>w, y<<w>>w
	}
	if false {
		if op == ir.Div {`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`s2_d01_division_shifts: interpreter`, `gen\d{4}: interpreter`}},
	{name: "4.5 shrq for signed >>", file: "internal/amd64/amd64.go",
		old: `case in.Type.Signed():
			op("movq\t%%rax, %%rdx")`, new: `case in.Type.Signed() && in.Op == ir.Shl:
			op("movq\t%%rax, %%rdx")`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`s2_d01_division_shifts: native`}},
	{name: "4.5 no negative-count check", file: "internal/lower/lower.go",
		old: `if !constant {
				neg :=`, new: `if !constant && false {
				neg :=`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`gen\d{4}: interpreter`, `gen\d{4}: native`}},

	// 4.6
	{name: "4.6 initialization order reversed", file: "internal/lower/lower.go",
		old: `for _, init := range order {`, new: `for i := range order {
		init := order[len(order)-1-i]`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`s2_g02_init_order: interpreter`, `gen\d{4}: interpreter`, `gen\d{4}: native`}},
	{name: "4.6 globals share one word", file: "internal/amd64/amd64.go",
		old: `%s:\n\t.zero\t8\n", sym, sym, sym)`, new: `%s:\n\t.zero\t0\n", sym, sym, sym)`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`gen\d{4}: native`}},

	// 4.7
	{name: "4.7 break leaves the enclosing for", file: "internal/lower/lower.go",
		old: `if s.Tok == token.BREAK {
				dest, t.broken`, new: `if s.Tok == token.BREAK && t.next != nil {
				dest, t.broken`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`s2_s01_switch: `, `gen\d{4}: `}},
	{name: "4.7 tag evaluated for each case value", file: "internal/lower/lower.go",
		old: `tag = l.spill(l.expr(s.Tag), l.pos(s.Tag, s.Tag.Pos()))`,
		new: `tag = func() *ir.Instr { return l.expr(s.Tag) }`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`s2_s01_switch: interpreter`, `s2_s01_switch: native`}},
	{name: "4.7 default taken in source order", file: "internal/lower/lower.go",
		old: `dflt, dfltBody = cc, bodies[i]
			continue`, new: `dflt, dfltBody = cc, bodies[i]
			next, pos := l.fn.NewBlock(), l.pos(cc, cc.Pos())
			l.fn.Branch(l.b, l.fn.TypedConst(l.b, ir.Bool, 1, pos), bodies[i], next, pos)
			l.b = next
			continue`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`s2_s01_switch: interpreter`, `gen\d{4}: interpreter`, `gen\d{4}: native`}},
	{name: "4.7 only the first value of each case tested", file: "internal/lower/lower.go",
		old: `for _, e := range cc.List {`, new: `for _, e := range cc.List[:1] {`,
		pkg: "./cmd/aether", run: "^TestDifferentialAgainstGc$", want: []string{`gen\d{4}: interpreter`, `gen\d{4}: native`}},

	// Instruction selection (D-029)
	selectControl("signed condition inverted wrongly", `"l": "ge"`, `"l": "g"`),
	selectControl("operands swapped without the condition", `ir.Lt: ir.Gt,`, `ir.Lt: ir.Lt,`),
	selectControl("unsigned comparison as signed", `if t := x.Type; t.IsInteger() && !t.Signed() {
		cc = strings`, `if t := x.Type; false && t.IsInteger() && !t.Signed() {
		cc = strings`),
	selectControl("fall-through jump not inverted", `jcc(inverse[cc], et)
	case !tc:`, `jcc(cc, et)
	case !tc:`),
	selectControl("fused Not ignored", `if c.neg {
		return inverse[cc]`, `if false {
		return inverse[cc]`),
	selectControl("forwarding past phi copies", `!e.hasCopies(ir.Edge{From: b, Index: 0}) {`, `true {`),
	selectControl("shift count equal to the width in range", `inRange := n < uint64(x.Type.Width())`, `inRange := n <= uint64(x.Type.Width())`),
	selectControl("no canonicalization with an immediate", `if in.Op == ir.Add || in.Op == ir.Sub || in.Op == ir.Mul {
		e.canonReg(t, in.Type)`, `if false {
		e.canonReg(t, in.Type)`),
	selectControl("subtraction of an immediate adds", `d, mnemonic = -c, "subq"`, `d, mnemonic = c, "subq"`),
	selectControl("and-not without the complement", `e.op("andq\t$%d, %s", ^c, t)`, `e.op("andq\t$%d, %s", c, t)`),
	selectControl("division by -1 as remainder", `c == -1 && in.Op == ir.Div:`, `c == -1 && in.Op == ir.Rem:`),
	selectControl("signedness of a right shift lost", `case x.Type.Signed():
		e.load(x, t)`, `case false:
		e.load(x, t)`),

	// Experiment ABI-R1 (D-030 criterion 7)
	r1Control("argument registers swapped", `regCopy{argRegs[i], e.val(a)}`, `regCopy{argRegs[i^1], e.val(a)}`),
	r1Control("entry reads the wrong register", `regCopy{e.val(in), argRegs[in.Index]}`, `regCopy{e.val(in), argRegs[(in.Index+1)%len(argRegs)]}`),
	r1Control("cycle break dropped", `e.op("movq\t%s, %%rax", d)`, ``),
	r1Control("stale result in %rax", `e.load(in.Args[0], "%rax")`, `_ = e.val(in.Args[0])`),
	r1Control("seventh argument read from a register", `e.toMem(fmt.Sprintf("%d(%%rbp)", 16+8*in.Index), e.val(in))`, `e.toMem(argRegs[in.Index%len(argRegs)], e.val(in))`),
	r1Control("frame missing a needed outgoing area", `if r1 && p <= len(argRegs) && r <= 1 {`, `if r1 {`),
	{name: "abi-r1 param after a clobbering instruction", file: "internal/amd64/abir1.go",
		old: `if in.Op != ir.Param && (in.Op != ir.Store || in.Args[0].Op != ir.Param) {`, new: `if false {`,
		pkg: "./internal/amd64", run: "^TestR1EntryPrecondition$", want: []string{`--- FAIL: TestR1EntryPrecondition`, `runs before the last param`}},
	{name: "abi-r1 entry with a predecessor", file: "internal/amd64/abir1.go",
		old: `if slices.Contains(ir.Succs(b), entry) {`, new: `if false {`,
		pkg: "./internal/amd64", run: "^TestR1EntryPrecondition$", want: []string{`--- FAIL: TestR1EntryPrecondition`, `entry block has predecessor`}},
}

// r1Control is a defect in abir1.go's emission, which checkR1 must report while
// the convention's programs are emitted.
func r1Control(name, old, new string) control {
	return control{name: "abi-r1 " + name, file: "internal/amd64/abir1.go", old: old, new: new,
		pkg: "./cmd/aether", run: "^TestABIR1Programs$", want: []string{`--- FAIL: TestABIR1Programs`, `abi-r1: `}}
}

// selectControl is a defect in select.go, which the differential test of
// selection must detect.
func selectControl(name, old, new string) control {
	return control{name: "isel " + name, file: "internal/amd64/select.go", old: old, new: new,
		pkg: "./cmd/aether", run: "^TestSelectDifferential$", want: []string{`--- FAIL: TestSelectDifferential`}}
}

// controlEnv is set in the environment of the tests a control runs, so
// that a copy of this test never runs controls itself.
const controlEnv = "AETHER_NEGATIVE_CONTROL"

// TestNegativeControls applies each control to its own copy of the module
// and runs the tests that must detect it: they must fail, with output
// matching the control's patterns. The working tree is never modified, so
// nothing needs restoring when a test fails. A baseline runs the same
// tests on an unmodified copy, which must pass, so that a failure is due to
// the defect and not to the copy.
func TestNegativeControls(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests modified copies of the module")
	}
	if os.Getenv(controlEnv) != "" {
		t.Skip("inside a negative control")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found: %v", err)
	}
	// Each control runs a test binary that may itself build and run many
	// programs; a few at a time keep memory use bounded.
	sem := make(chan struct{}, 4)
	t.Run("baseline", func(t *testing.T) {
		t.Parallel()
		sem <- struct{}{}
		defer func() { <-sem }()
		runs := map[string][]string{}
		for _, c := range controls {
			runs[c.pkg] = append(runs[c.pkg], strings.Trim(c.run, "^$"))
		}
		dir := copyModule(t, root)
		pkgs := make([]string, 0, len(runs))
		for pkg := range runs {
			pkgs = append(pkgs, pkg)
		}
		sort.Strings(pkgs)
		for _, pkg := range pkgs {
			pattern := "^(" + strings.Join(runs[pkg], "|") + ")$"
			if out, err := goTest(gobin, dir, pkg, pattern, false); err != nil {
				t.Fatalf("unmodified copy fails %s -run %s: %v\n%s", pkg, pattern, err, out)
			}
		}
	})
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sem <- struct{}{}
			defer func() { <-sem }()
			dir := copyModule(t, root)
			path := filepath.Join(dir, filepath.FromSlash(c.file))
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(src), c.old); n != 1 {
				t.Fatalf("%s: the text to replace occurs %d times, not once", c.file, n)
			}
			writeFile(t, path, []byte(strings.Replace(string(src), c.old, c.new, 1)))
			out, err := goTest(gobin, dir, c.pkg, c.run, c.interpOnly)
			if _, failed := err.(*exec.ExitError); !failed {
				t.Fatalf("not detected: %s -run %s passed with the defect (%v)\n%s", c.pkg, c.run, err, out)
			}
			if strings.Contains(out, "[build failed]") || strings.Contains(out, "[setup failed]") {
				t.Fatalf("the modified copy does not build, which proves nothing:\n%s", out)
			}
			for _, w := range c.want {
				if !regexp.MustCompile(w).MatchString(out) {
					t.Errorf("failing output does not match %q:\n%s", w, out)
				}
			}
		})
	}
}

// goTest runs "go test -run pattern pkg" in the module copy dir and
// returns its combined output. With interpOnly, PATH holds only the go
// command, so GNU as and ld are not found.
func goTest(gobin, dir, pkg, pattern string, interpOnly bool) (string, error) {
	cmd := exec.Command(gobin, "test", "-count=1", "-timeout=10m", "-run", pattern, pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOTOOLCHAIN=local", controlEnv+"=1")
	if interpOnly {
		bin := filepath.Join(dir, ".controlbin")
		if err := os.Mkdir(bin, 0o755); err != nil {
			return "", err
		}
		if err := os.Symlink(gobin, filepath.Join(bin, "go")); err != nil {
			return "", err
		}
		cmd.Env = append(cmd.Env, "PATH="+bin, "CGO_ENABLED=0")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// copyModule copies the module at root, without .git, to a new temporary
// directory and returns it.
func copyModule(t *testing.T, root string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir() && d.Name() == ".git":
			return filepath.SkipDir
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		case !d.Type().IsRegular():
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}
