package amd64

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
	"github.com/stevenstank/aether/internal/ir"
)

var update = flag.Bool("update", false, "rewrite testdata/*.s")

var p1 = ir.Pos{Line: 1, Col: 1}

// program returns a main whose body is built by body, then ends in ret.
func program(body func(f *ir.Func, b *ir.Block)) *ir.Program {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	body(f, f.Entry())
	f.Ret(f.Entry(), p1)
	return p
}

// sample uses every IR operation.
func sample() *ir.Program {
	return program(func(f *ir.Func, b *ir.Block) {
		x := f.NewSlot("x", ir.I64, p1)
		f.Store(b, x, f.Const(b, 6, p1), p1)
		lx := f.Load(b, x, p1)
		big := f.Const(b, math.MinInt64, p1)
		sum := f.Binary(b, ir.Add, lx, big, p1)
		diff := f.Binary(b, ir.Sub, sum, lx, p1)
		prod := f.Binary(b, ir.Mul, diff, f.Neg(b, lx, p1), p1)
		f.Println(b, []*ir.Instr{lx, prod}, p1)
		f.Println(b, nil, p1)
	})
}

// TestEmitGolden pins the instruction selection and frame layout for every
// IR operation (testdata/sample.s, updated with -update).
func TestEmitGolden(t *testing.T) {
	got, err := Emit(sample())
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := Emit(sample()); !bytes.Equal(got, again) {
		t.Fatal("emission is not deterministic")
	}
	golden := filepath.Join("testdata", "sample.s")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("assembly differs from %s:\n%s", golden, got)
	}
}

func TestConstantSelection(t *testing.T) {
	tests := []struct {
		c    int64
		want string
	}{
		{0, "movq\t$0, -8(%rbp)"},
		{math.MaxInt32, "movq\t$2147483647, -8(%rbp)"},
		{math.MinInt32, "movq\t$-2147483648, -8(%rbp)"},
		{math.MaxInt32 + 1, "movabsq\t$2147483648, %rax\n\tmovq\t%rax, -8(%rbp)"},
		{math.MinInt32 - 1, "movabsq\t$-2147483649, %rax\n\tmovq\t%rax, -8(%rbp)"},
		{math.MaxInt64, "movabsq\t$9223372036854775807, %rax"},
		{math.MinInt64, "movabsq\t$-9223372036854775808, %rax"},
	}
	for _, tt := range tests {
		asm, err := Emit(program(func(f *ir.Func, b *ir.Block) {
			f.Println(b, []*ir.Instr{f.Const(b, tt.c, p1)}, p1)
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(asm), tt.want) {
			t.Errorf("const %d: want %q in\n%s", tt.c, tt.want, asm)
		}
	}
}

func TestFrame(t *testing.T) {
	tests := []struct{ slots, values, size int }{
		{0, 0, 0}, {1, 0, 16}, {2, 0, 16}, {1, 2, 32}, {3, 3, 48}, {0, 5, 48},
	}
	for _, tt := range tests {
		fr, err := NewFrame(tt.slots, tt.values)
		if err != nil || fr.Size != tt.size || fr.Size%16 != 0 {
			t.Errorf("NewFrame(%d, %d) = %+v, %v; want size %d", tt.slots, tt.values, fr, err, tt.size)
		}
	}
	fr, _ := NewFrame(2, 3)
	if fr.SlotOffset(0) != -8 || fr.SlotOffset(1) != -16 || fr.ValueOffset(0) != -24 || fr.ValueOffset(2) != -40 {
		t.Errorf("offsets: s0 %d s1 %d v0 %d v2 %d", fr.SlotOffset(0), fr.SlotOffset(1), fr.ValueOffset(0), fr.ValueOffset(2))
	}
	// The deepest offset must lie inside the frame.
	if -fr.ValueOffset(fr.Values-1) > fr.Size {
		t.Errorf("value v%d at %d is outside a frame of %d bytes", fr.Values-1, fr.ValueOffset(fr.Values-1), fr.Size)
	}
	for _, bad := range [][2]int{{-1, 0}, {0, -1}, {maxFrame / 8, 1}, {1, maxFrame / 8}, {math.MaxInt, math.MaxInt}} {
		if _, err := NewFrame(bad[0], bad[1]); err == nil {
			t.Errorf("NewFrame(%d, %d) accepted", bad[0], bad[1])
		}
	}
}

func TestEmitRejects(t *testing.T) {
	malformed := program(func(f *ir.Func, b *ir.Block) {
		f.Load(b, f.NewSlot("x", ir.I64, p1), p1) // load before store
	})
	if asm, err := Emit(malformed); err == nil || asm != nil || !strings.Contains(err.Error(), "malformed IR") {
		t.Errorf("malformed IR: %v, %d bytes", err, len(asm))
	}
	two := sample()
	f := two.NewFunc("f", "p.go", p1)
	f.Ret(f.Entry(), p1)
	if asm, err := Emit(two); err == nil || asm != nil || !strings.Contains(err.Error(), "func f: only main is supported") {
		t.Errorf("second function: %v, %d bytes", err, len(asm))
	}
}

func TestRuntimeFiles(t *testing.T) {
	files := Runtime()
	var names []string
	all := ""
	for _, f := range files {
		names = append(names, f.Name)
		all += string(f.Data)
	}
	if strings.Join(names, " ") != "print.s start.s" {
		t.Fatalf("runtime files %v", names)
	}
	for _, sym := range []string{"_start", symPrintInt, symPrintSpace, symPrintNL} {
		if !strings.Contains(all, "\t.globl\t"+sym+"\n") || !strings.Contains(all, "\n"+sym+":\n") {
			t.Errorf("runtime does not define %s", sym)
		}
	}
	if strings.Contains(all, "\nmain.main:") {
		t.Error("runtime defines main.main")
	}
}

// link assembles and links the files in dir the way the driver does, and
// returns the executable's path. It skips the test without GNU tools.
func link(t *testing.T, dir string, asm map[string][]byte) string {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("executables need a linux/amd64 host")
	}
	as, errAs := exec.LookPath("as")
	ld, errLd := exec.LookPath("ld")
	if errAs != nil || errLd != nil {
		t.Skipf("GNU as and ld not found: %v %v", errAs, errLd)
	}
	var objs []string
	for name, data := range asm {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		obj := strings.TrimSuffix(name, ".s") + ".o"
		run(t, dir, as, "--64", "-o", obj, name)
		objs = append(objs, obj)
	}
	run(t, dir, ld, append([]string{"-static", "-nostdlib", "-e", "_start", "-o", "a.out"}, objs...)...)
	return filepath.Join(dir, "a.out")
}

func run(t *testing.T, dir, tool string, args ...string) {
	t.Helper()
	cmd := exec.Command(tool, args...)
	cmd.Dir = dir
	cmd.Env = []string{"LC_ALL=C"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", tool, args, err, out)
	}
}

// execute runs the executable and returns its stderr and exit status. Its
// stdout must be empty.
func execute(t *testing.T, exe string, stderrFile *os.File) (string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(exe)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stderrFile != nil {
		cmd.Stderr = stderrFile
	}
	code := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout: %q", stdout.String())
	}
	return stderr.String(), code
}

func withRuntime(prog []byte) map[string][]byte {
	files := map[string][]byte{"prog.s": prog}
	for _, f := range Runtime() {
		files[f.Name] = f.Data
	}
	return files
}

// TestNativeMatchesInterpreter runs IR exercising every operation at the
// integer boundaries both natively and in the interpreter, and requires
// identical output.
func TestNativeMatchesInterpreter(t *testing.T) {
	edges := []int64{0, 1, -1, 2, 7, -10, math.MaxInt32, math.MinInt32, 3037000500, math.MaxInt64, math.MinInt64}
	prog := program(func(f *ir.Func, b *ir.Block) {
		s := f.NewSlot("s", ir.I64, p1)
		for _, x := range edges {
			f.Store(b, s, f.Const(b, x, p1), p1)
			f.Println(b, []*ir.Instr{f.Load(b, s, p1), f.Neg(b, f.Load(b, s, p1), p1)}, p1)
			for _, y := range edges {
				var args []*ir.Instr
				for _, op := range []ir.Op{ir.Add, ir.Sub, ir.Mul} {
					args = append(args, f.Binary(b, op, f.Load(b, s, p1), f.Const(b, y, p1), p1))
				}
				f.Println(b, args, p1)
			}
		}
		f.Println(b, nil, p1)
	})
	var want bytes.Buffer
	if err := interp.Run(prog, &want); err != nil {
		t.Fatal(err)
	}
	asm, err := Emit(prog)
	if err != nil {
		t.Fatal(err)
	}
	got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil)
	if code != 0 || got != want.String() {
		t.Errorf("exit %d; native output differs from the interpreter's\nnative:\n%s\ninterpreter:\n%s", code, got, want.String())
	}
	if !strings.Contains(got, "-9223372036854775808 -9223372036854775808\n") {
		t.Error("output lacks MinInt64 and its negation")
	}
}

// abiHarness calls each runtime helper with sentinel values in every
// register the helpers must preserve, and exits with status 1 if any of
// them, or %rsp, changed. Each call is made with %rsp 16-byte aligned.
var abiHarness = `	.text
	.globl	main.main
main.main:
	pushq	%rbp
	movq	%rsp, %rbp
	pushq	%rbx
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	subq	$8, %rsp
	movq	$0x1111, %rbx
	movq	$0x1212, %r12
	movq	$0x1313, %r13
	movq	$0x1414, %r14
	movq	$0x1515, %r15
	movq	%rsp, %r8
	movq	%r8, -48(%rbp)
	movabsq	$-9223372036854775808, %rdi
	call	aether.printint
	call	aether.printsp
	movabsq	$9223372036854775807, %rdi
	call	aether.printint
	call	aether.printsp
	movq	$0, %rdi
	call	aether.printint
	call	aether.printsp
	movq	$-1000000, %rdi
	call	aether.printint
	call	aether.printnl
	cmpq	$0x1111, %rbx
	jne	1f
	cmpq	$0x1212, %r12
	jne	1f
	cmpq	$0x1313, %r13
	jne	1f
	cmpq	$0x1414, %r14
	jne	1f
	cmpq	$0x1515, %r15
	jne	1f
	cmpq	-48(%rbp), %rsp
	jne	1f
	leaq	-48(%rbp), %rax
	cmpq	%rax, %rsp
	jne	1f
	addq	$8, %rsp
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbx
	popq	%rbp
	ret
1:	movl	$231, %eax
	movl	$1, %edi
	syscall
` + mainStack(64)

// mainStack declares the stack main.main uses below _start's call, as the
// backend does (symMainStack), and ends the file.
func mainStack(n int) string {
	return fmt.Sprintf("\t.section\t.rodata\n\t.globl\t%s\n%s:\n\t.quad\t%d\n\t.section\t.note.GNU-stack,\"\",@progbits\n",
		symMainStack, symMainStack, n)
}

// TestRuntimeABI checks the runtime-helper interface: formatting of the
// extreme values, preservation of %rbx, %rbp, %r12-%r15, and %rsp, and that
// write errors are ignored (the program still exits 0).
func TestRuntimeABI(t *testing.T) {
	exe := link(t, t.TempDir(), withRuntime([]byte(abiHarness)))
	got, code := execute(t, exe, nil)
	if want := "-9223372036854775808 9223372036854775807 0 -1000000\n"; code != 0 || got != want {
		t.Errorf("exit %d, stderr %q; want exit 0, %q", code, got, want)
	}
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no /dev/full: %v", err)
	}
	defer full.Close()
	if _, code := execute(t, exe, full); code != 0 {
		t.Errorf("with stderr on /dev/full: exit %d, want 0", code)
	}
}

// stackHarness has a frame of the given size: it writes the frame's lowest
// word, then calls aether.printint and aether.printnl from the bottom of the
// frame, so the helpers run below it.
func stackHarness(frame int) string {
	return fmt.Sprintf(`	.text
	.globl	main.main
main.main:
	pushq	%%rbp
	movq	%%rsp, %%rbp
	subq	$%d, %%rsp
	movq	$1, (%%rsp)
	movq	$-5, %%rdi
	call	aether.printint
	call	aether.printnl
	movq	%%rbp, %%rsp
	popq	%%rbp
	ret
`, frame) + mainStack(16+frame)
}

// limited runs exe through /bin/sh with the given ulimit options, and
// returns its stderr and exit status.
func limited(t *testing.T, exe string, limits ...string) (string, int) {
	t.Helper()
	script := ""
	for _, l := range limits {
		script += "ulimit " + l + " && "
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("/bin/sh", "-c", script+`exec "$0"`, exe)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		if exit.ExitCode() < 0 {
			t.Fatalf("%s: %v", exe, err) // killed by a signal
		}
		code = exit.ExitCode()
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout: %q", stdout.String())
	}
	return stderr.String(), code
}

// TestRuntimeStack checks that _start runs main.main on a stack it maps to
// fit aether.mainstack, independent of the process stack limit (F1), and
// that a stack it cannot allocate is a fatal error with exit status 2,
// never a signal.
func TestRuntimeStack(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh to set limits with")
	}
	const fatal = "fatal error: cannot allocate the main stack\n"
	tests := []struct {
		name   string
		frame  int
		limits []string
		want   string
		code   int
	}{
		{"small frame, 256 KiB stack limit", 64, []string{"-s 256"}, "-5\n", 0},
		{"64 MiB frame, 1 MiB stack limit", 64 << 20, []string{"-s 1024"}, "-5\n", 0},
		{"maximum frame, 1 MiB stack limit", maxFrame, []string{"-s 1024"}, "-5\n", 0},
		{"no address space for the stack", 256 << 20, []string{"-v 65536"}, fatal, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe := link(t, t.TempDir(), withRuntime([]byte(stackHarness(tt.frame))))
			got, code := limited(t, exe, tt.limits...)
			if tt.frame == maxFrame && got == fatal {
				// A 1 GiB private mapping can be refused under strict
				// overcommit; the program must still fail cleanly.
				t.Skipf("this host refused a %d-byte stack mapping: %q, exit %d", tt.frame, got, code)
			}
			if got != tt.want || code != tt.code {
				t.Errorf("stderr %q, exit %d; want %q, exit %d", got, code, tt.want, tt.code)
			}
		})
	}
}
