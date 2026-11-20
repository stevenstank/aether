package main

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
)

// gcStressEnv makes the collector run every few KiB and overwrite what it
// frees (D-046).
var gcStressEnv = []string{"AETHER_GCSTRESS=1"}

// generatedAllocPrograms is how many allocation programs (generateAllocs)
// TestGCStress runs.
const generatedAllocPrograms = 60

// TestGCStress runs, under the collector's stress mode, every corpus
// program expected to run, allocation programs (generateAllocs), and
// programs of every other generator, built by aether build and with -O0,
// and requires gc's output (D-046). A reachable object freed by mistake is
// overwritten, so it changes the output or crashes the program. It also
// checks the roots' premise: .bss is each executable's only writable
// section.
func TestGCStress(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	progs := map[string][]byte{}
	for _, path := range corpus(t) {
		if readExpectation(t, path).class != "run" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		progs[strings.TrimSuffix(filepath.Base(path), ".go")] = src
	}
	for name, src := range gcPrograms {
		progs[name] = []byte(src)
	}
	for seed := uint64(1); seed <= generatedAllocPrograms; seed++ {
		progs[fmt.Sprintf("alc%04d", seed)] = []byte(generateAllocs(seed))
	}
	for seed := uint64(1); seed <= 10; seed++ {
		for prefix, gen := range map[string]func(uint64) string{
			"slc": generateSlices, "mth": generateMethods, "fnv": generateFuncValues, "cls": generateClosures,
			"ifc": generateIfaces, "map": generateMaps, "nmd": generateNamed, "gnr": generateGenerics,
		} {
			progs[fmt.Sprintf("%s%04d", prefix, seed)] = []byte(gen(seed))
		}
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
		if _, ok := want[name]; !ok {
			continue // a generated program with an overflowing constant
		}
		for _, o0 := range []bool{false, true} {
			exe := filepath.Join(dir, fmt.Sprintf("%s.%v", name, o0))
			if ds, err := build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
				t.Fatalf("%s: -O0 %v: not built: %v %v", name, o0, ds, err)
			}
			if !o0 {
				checkWritable(t, exe)
			}
			if got := runExeEnv(t, exe, gcStressEnv); got != want[name] {
				t.Errorf("%s: -O0 %v: under GC stress, differs from gc:\n%s", name, o0, diffResults(got, want[name]))
			}
		}
	}
	t.Logf("%d programs", len(progs))
}

// checkWritable fails unless .bss is the only writable section exe loads:
// the collector's roots are the stack and .bss (D-046).
func checkWritable(t *testing.T, exe string) {
	t.Helper()
	f, err := elf.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, s := range f.Sections {
		if s.Flags&elf.SHF_ALLOC != 0 && s.Flags&elf.SHF_WRITE != 0 && s.Name != ".bss" {
			t.Errorf("%s: writable section %s, which the collector does not scan", exe, s.Name)
		}
	}
}

// gcPrograms are TestGCStress's own programs.
var gcPrograms = map[string]string{
	// Only pointers inside the objects keep them alive: a tail of a
	// slice, and a pointer to a struct's field.
	"interior": `package main

type P struct{ a, b [40]int }

func mk() []int {
	s := make([]int, 100)
	for i := 0; i < 100; i++ {
		s[i] = i
	}
	return s[50:]
}

func mkp() *[40]int {
	p := &P{}
	p.b[3] = 7
	return &p.b
}

func main() {
	t := mk()
	q := mkp()
	acc := 0
	for i := 0; i < 100000; i++ {
		x := make([]int, 10)
		x[0] = i
		acc += x[0]
	}
	println(t[0], t[49], q[3], acc)
}
`,
}

// gcWorkloads are programs that allocate gigabytes over time while
// keeping little alive, for TestGCMemoryBound.
var gcWorkloads = map[string]string{
	"churn": `package main

type node struct {
	next *node
	v    int
	s    string
}

func build(n, k int) *node {
	var h *node
	for i := 0; i < n; i++ {
		h = &node{h, i * k, string(rune('a' + i%26))}
	}
	return h
}

func sum(h *node) int {
	t := 0
	for ; h != nil; h = h.next {
		t += h.v + len(h.s)
	}
	return t
}

func main() {
	keep := build(10000, 1)
	total := 0
	for r := 0; r < 20000; r++ {
		l := build(1000, r)
		total += sum(l)
		m := map[int][]int{}
		for i := 0; i < 20; i++ {
			m[i] = make([]int, 100+i)
			m[i][i] = r
		}
		total += m[r%20][r%20]
		s := ""
		for i := 0; i < 20; i++ {
			s += "xy"
		}
		total += len(s)
	}
	println(total, sum(keep))
}
`,
	"large": `package main

func main() {
	var keep [][]byte
	total := 0
	for r := 0; r < 4000; r++ {
		b := make([]byte, 1<<20+r*16)
		b[r] = byte(r)
		total += int(b[r]) + len(b)
		if r%500 == 0 {
			keep = append(keep, b)
		}
		var ps []*[100]int
		for i := 0; i < 200; i++ {
			ps = append(ps, &[100]int{i})
		}
		total += ps[r%200][0]
	}
	println(total, len(keep), len(keep[3]))
}
`,
	// A select that blocks two million times on a reply and on a channel
	// never used: each time, its waiter on the idle channel must leave
	// that queue (D-048, increment 10.3), or the queue keeps them all
	// (328 MiB, measured 2026-11-20). req is buffered so that main reaches
	// the select before the reply is sent, and blocks in it.
	"select": `package main

func main() {
	req, resp, idle := make(chan int, 1), make(chan int), make(chan int)
	go func() {
		for v := range req {
			resp <- v * 2
		}
	}()
	total := 0
	for i := 0; i < 2000000; i++ {
		req <- i
		select {
		case v := <-resp:
			total += v
		case <-idle:
			total = -1
		}
	}
	println(total)
}
`,
}

// gcMemoryBound is what the workloads' peak resident memory must stay
// below. Each allocates several GiB in all.
const gcMemoryBound = 64 << 20

// TestGCMemoryBound runs long allocation workloads built by aether build:
// their output must be gc's, and their peak resident memory below
// gcMemoryBound (D-046), where without collection it would be gigabytes.
func TestGCMemoryBound(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with gc")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	progs := map[string][]byte{}
	for name, src := range gcWorkloads {
		progs[name] = []byte(src)
	}
	want := gcRun(t, gobin, progs)
	dir := t.TempDir()
	for _, name := range slices.Sorted(maps.Keys(progs)) {
		exe := filepath.Join(dir, name)
		if ds, err := build([]source{{name + ".go", progs[name]}}, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("%s: not built: %v %v", name, ds, err)
		}
		// The workload is started by a fresh process of this test binary,
		// TestGCRSSHelper: a child started from this process would count
		// this process's memory in its peak (Linux keeps the peak of the
		// address space a child shares until exec).
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(os.Args[0], "-test.run=^TestGCRSSHelper$")
		cmd.Env = append(os.Environ(), "AETHER_RSS_HELPER="+exe)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, stderr.String())
		}
		var rss int64
		var out string
		if _, err := fmt.Sscanf(stdout.String(), "maxrss %d\n", &rss); err != nil {
			t.Fatalf("%s: helper output %q: %v", name, stdout.String(), err)
		}
		out = stderr.String()
		if got := (result{stderr: out}); got != want[name] {
			t.Errorf("%s: differs from gc:\n%s", name, diffResults(got, want[name]))
		}
		t.Logf("%s: peak resident memory at most %d KiB (with the helper's own)", name, rss>>10)
		if rss > gcMemoryBound {
			t.Errorf("%s: peak resident memory %d bytes, above the bound %d", name, rss, gcMemoryBound)
		}
	}
}

// TestGCRSSHelper, run by TestGCMemoryBound in a fresh process, runs the
// executable named by AETHER_RSS_HELPER, copies its stderr, and writes its
// peak resident memory in bytes to stdout.
func TestGCRSSHelper(t *testing.T) {
	exe := os.Getenv("AETHER_RSS_HELPER")
	if exe == "" {
		t.Skip("run by TestGCMemoryBound")
	}
	cmd := exec.Command(exe)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("maxrss %d\n", cmd.ProcessState.SysUsage().(*syscall.Rusage).Maxrss<<10)
}

// FuzzGC is the collector's allocation fuzzing (Phase 9 increment 9.3):
// the allocation program of each seed (generateAllocs), built by aether
// build and run under GC stress, must give the interpreter's output. The
// interpreter never frees (D-010), so it is an oracle for the collector
// that needs no gc build per input.
func FuzzGC(f *testing.F) {
	for seed := uint64(1); seed <= 4; seed++ {
		f.Add(seed)
	}
	requireTools(f)
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, seed uint64) {
		src := []byte(generateAllocs(seed))
		want, ds, err := interpret("alloc.go", src)
		if errors.Is(err, interp.ErrSteps) {
			t.Skip("longer than the interpreter's step budget: no oracle")
		}
		if len(ds) > 0 || err != nil {
			t.Fatalf("not interpreted: %v %v\n%s", ds, err, src)
		}
		exe := filepath.Join(dir, fmt.Sprintf("alc%d", seed))
		if ds, err := build([]source{{"alloc.go", src}}, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("not built: %v %v", ds, err)
		}
		defer os.Remove(exe)
		if got := runExeEnv(t, exe, gcStressEnv); got != want {
			t.Errorf("under GC stress, differs from the interpreter:\n%s\n%s", diffResults(got, want), src)
		}
	})
}
