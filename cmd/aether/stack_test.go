package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// runLimited runs exe through /bin/sh with the given ulimit options. A
// signal, such as SIGSEGV from a stack overflow, or a run longer than
// runTimeout fails the test.
func runLimited(t *testing.T, exe string, limits ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	script := ""
	for _, l := range limits {
		script += "ulimit " + l + " && "
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script+`exec "$0"`, exe)
	cmd.WaitDelay = waitDelay
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := result{}
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("%s with ulimit %v: killed after running for the %v timeout", exe, limits, runTimeout)
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() < 0 {
			t.Fatalf("%s with ulimit %v: %v", exe, limits, err)
		}
		r.exit = exit.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// TestBuildLargeFrame is the regression test for F1: a valid S1 program
// whose frame is larger than the default 8 MiB process stack built
// successfully, but its executable died with SIGSEGV. The stack is now mapped
// by the runtime to fit the frame, so the program runs under the default
// and under a reduced stack limit, like gc's. A small program is checked
// under the same limits.
func TestBuildLargeFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2 MB program")
	}
	requireTools(t)
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh to set limits with")
	}
	// One value per argument: a frame of at least 8*n = 8.8 MB. The function
	// is over the limit on promotion (D-027), so each operand is a load with
	// a cell. Constants would take no cells (D-029).
	const n = 1_100_000
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	args := strings.Repeat("x, ", n-1) + "x"
	writeFile(t, src, []byte("package main\n\nfunc main() {\n\tx := 0\n\tprintln("+args+")\n}\n"))
	big := filepath.Join(dir, "big")
	if stderr, code := runAether("build", "-o", big, src); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	small := filepath.Join(dir, "small")
	if stderr, code := runAether("build", "-o", small, corpusFile("s1_03_milestone1")); code != 0 {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	for _, limit := range []string{"-s 8192", "-s 1024"} {
		if got, want := runLimited(t, big, limit), (result{stderr: strings.Repeat("0 ", n-1) + "0\n"}); got != want {
			t.Errorf("large frame, ulimit %s: exit %d, %d bytes of stderr, stdout %q; want exit 0, %d bytes",
				limit, got.exit, len(got.stderr), got.stdout, len(want.stderr))
		}
		if got, want := runLimited(t, small, limit), (result{stderr: "-6 42\n6 -9223372036854775808\n"}); got != want {
			t.Errorf("small frame, ulimit %s: %+v, want %+v", limit, got, want)
		}
	}
}

// deepSrc recurses through a function with several cases, each with
// temporaries of its own, as an interpreter's evaluator does: 1,000,000
// levels on the main stack, and 200,000 on a goroutine's 64 MiB (D-048).
// With a cell for every value and a word for every slot, its frame was
// 272 bytes, and 3,536 with -O0: both overflowed (V-2), where gc's stack
// holds it.
const deepSrc = `package main

import (
	"errors"
	"strconv"
)

type node struct {
	op   int
	a, b *node
}

func eval(n int, k *node) int {
	if n == 0 {
		return 0
	}
	switch k.op % 6 {
	case 0:
		x, y := n*3, n^7
		return eval(n-1, k.a) + x - y
	case 1:
		var s string
		if n%5000 == 0 {
			s = strconv.Itoa(n)
		}
		return eval(n-1, k.b) + len(s)
	case 2:
		v := [3]int{n, n + 1, n + 2}
		r := eval(n-1, k.a)
		return r + v[0] - v[2]
	case 3:
		var m map[int]int
		if n%5000 == 0 {
			m = map[int]int{n: 1}
		}
		return eval(n-1, k.b) + m[n]
	case 4:
		p, q, r := n/3, n%3, n<<1
		if p > q {
			return eval(n-1, k.a) + r
		}
		return eval(n-1, k.b) - r
	default:
		var e error
		if n%5000 == 0 {
			e = errors.New("e")
		}
		if e != nil {
			return eval(n-1, k.a) + len(e.Error())
		}
		return eval(n-1, k.a) - 1
	}
}

func main() {
	nodes := make([]*node, 6)
	for i := range nodes {
		nodes[i] = &node{op: i}
	}
	for i, k := range nodes {
		k.a, k.b = nodes[(i+1)%6], nodes[(i+5)%6]
	}
	println(eval(1000000, nodes[0]))
	done := make(chan int)
	go func() { done <- eval(200000, nodes[3]) }()
	println(<-done)
}
`

// TestDeepRecursion is the regression test for the depth of the V-2
// failures of practical evaluation v2 (09-scheme's deep and control): with
// cells and slots shared by values never live at once (D-058), deepSrc
// runs to gc's result with and without -O0.
func TestDeepRecursion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	want := gcRun(t, gobin, map[string][]byte{"deep": []byte(deepSrc)})["deep"]
	if want.exit != 0 || want.stderr == "" {
		t.Fatalf("gc: %+v", want)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "deep.go")
	writeFile(t, src, []byte(deepSrc))
	for _, o0 := range []bool{false, true} {
		out := filepath.Join(dir, "deep")
		args := []string{"build", "-o", out}
		if o0 {
			args = append(args, "-O0")
		}
		if stderr, code := runAether(append(args, src)...); code != 0 {
			t.Fatalf("aether %v: exit %d\n%s", args, code, stderr)
		}
		if got := runExe(t, out); got != want {
			t.Errorf("-O0 %v: aether %+v, gc %+v", o0, got, want)
		}
	}
}

// TestSharedCellsNoLarger checks that sharing cells and slots (D-058)
// never makes a function's frame larger, or its code longer, than a cell
// for each value and a word for each slot, with and without -O0, on the
// S2 suite and the workloads; it logs what sharing saves.
func TestSharedCellsNoLarger(t *testing.T) {
	progs, _ := s2Suite(t)
	for _, w := range benchWorkloads(t) {
		progs = append(progs, s2Program{w.name + ".go", w.src})
	}
	for _, o0 := range []bool{false, true} {
		var unique, shared codeStats
		for _, p := range progs {
			emit := func(uniqueCells bool) []byte {
				prog, ds := compile([]source{{p.name, p.src}})
				if len(ds) > 0 {
					t.Fatal(ds)
				}
				var asm []byte
				var err error
				if o0 {
					asm, err = amd64.EmitWith(prog, amd64.EmitOptions{Verified: true, UniqueCells: uniqueCells})
				} else {
					asm, err = emitProgramWith(prog, amd64.EmitOptions{Registers: true, Select: true, ABIR1: true, TwoAddress: true, Coalesce: true, UniqueCells: uniqueCells})
				}
				if err != nil {
					t.Fatalf("%s: %v", p.name, err)
				}
				return asm
			}
			u, uStack := measureAsm(t, emit(true))
			s, sStack := measureAsm(t, emit(false))
			unique.mainStack += uStack
			shared.mainStack += sStack
			for _, fn := range slices.Sorted(maps.Keys(u)) {
				unique.add(u[fn])
				shared.add(s[fn])
				if s[fn].instrs > u[fn].instrs || s[fn].frame > u[fn].frame {
					t.Errorf("-O0 %v: %s: %s: %d instructions and frame %d shared, %d and %d unshared",
						o0, p.name, fn, s[fn].instrs, s[fn].frame, u[fn].instrs, u[fn].frame)
				}
			}
		}
		t.Logf("-O0 %v: frame bytes: unshared %d, shared %d (%s); largest frame %d, %d; machine instructions %d, %d (%s)",
			o0, unique.frame, shared.frame, ratio(shared.frame, unique.frame), unique.maxFrame, shared.maxFrame,
			unique.instrs, shared.instrs, ratio(shared.instrs, unique.instrs))
	}
}
