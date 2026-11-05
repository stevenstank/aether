package main

import (
	"debug/elf"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/ir"
)

// funcCode is what the static measurement of the D-006 experiment records
// for one function of one program, under one backend input.
type funcCode struct {
	instrs int // machine instructions in the emitted assembly
	bytes  int // size of the function's code in the linked executable
	frame  int // frame size: the prologue's subq from %rsp, or 0
}

// codeStats sums funcCode over functions.
type codeStats struct {
	funcs, instrs, bytes, frame, maxFrame, mainStack int
}

func (s *codeStats) add(c funcCode) {
	s.funcs++
	s.instrs += c.instrs
	s.bytes += c.bytes
	s.frame += c.frame
	s.maxFrame = max(s.maxFrame, c.frame)
}

var (
	funcType   = regexp.MustCompile(`^\t\.type\t(main\.\w+), @function$`)
	frameAlloc = regexp.MustCompile(`^\tsubq\t\$(\d+), %rsp$`)
	mainStack  = regexp.MustCompile(`aether\.mainstack:\n\t\.quad\t(\d+)\n`)
)

// measureAsm counts the machine instructions and finds the frame size of
// each function in asm, Emit's output. A function runs from its .type
// directive to its .size directive; every line in it that begins with a
// tab and is neither a directive nor a comment is one instruction. It also
// returns the value of aether.mainstack.
func measureAsm(t *testing.T, asm []byte) (map[string]funcCode, int) {
	t.Helper()
	funcs := map[string]funcCode{}
	var cur string
	for _, line := range strings.Split(string(asm), "\n") {
		if m := funcType.FindStringSubmatch(line); m != nil {
			cur = m[1]
			funcs[cur] = funcCode{}
			continue
		}
		if cur == "" || !strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "\t.") || strings.HasPrefix(line, "\t#") {
			if strings.HasPrefix(line, "\t.size\t") {
				cur = ""
			}
			continue
		}
		c := funcs[cur]
		c.instrs++
		if m := frameAlloc.FindStringSubmatch(line); m != nil && c.frame == 0 {
			c.frame, _ = strconv.Atoi(m[1])
		}
		funcs[cur] = c
	}
	m := mainStack.FindSubmatch(asm)
	if m == nil {
		t.Fatal("no aether.mainstack in the assembly")
	}
	stack, _ := strconv.Atoi(string(m[1]))
	return funcs, stack
}

// funcBytes returns the size of every main.* function symbol in the ELF
// executable exe.
func funcBytes(t *testing.T, exe string) map[string]int {
	t.Helper()
	f, err := elf.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	syms, err := f.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]int{}
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) == elf.STT_FUNC && strings.HasPrefix(s.Name, "main.") {
			sizes[s.Name] = int(s.Size)
		}
	}
	return sizes
}

// TestNativeSSASuite is the native part of the D-006 experiment (part 4).
// Every program of the S2 suite is compiled twice with the same settings:
// as lowered, with slots, and with every function promoted to SSA form
// (D-025). Both are built into executables with the same runtime and
// tools; the two executables and the interpreter, on the unpromoted IR,
// must give the same stderr and exit status. It then logs the static
// measurement of both: machine instructions in the assembly, code bytes
// of the program's functions in the executable, frame sizes, and the
// stack aether.mainstack reserves for main or init. These are static
// counts; nothing here measures run time. Setting AETHER_SSA_REPORT to a
// file path also writes the per-function figures there.
func TestNativeSSASuite(t *testing.T) {
	if testing.Short() {
		t.Skip("builds about 480 executables")
	}
	requireTools(t)
	progs, corpusRun := s2Suite(t)
	dir := t.TempDir()
	var slot, ssa codeStats
	var report strings.Builder
	report.WriteString("program\tfunc\tslot instrs\tssa instrs\tslot bytes\tssa bytes\tslot frame\tssa frame\n")
	regress := map[string]int{}
	cycles := 0
	for _, p := range progs {
		want, ds, err := interpret(p.name, p.src)
		if len(ds) > 0 || err != nil {
			t.Fatalf("%s: not interpreted: %v %v", p.name, ds, err)
		}
		var code [2]map[string]funcCode
		var stack [2]int
		for i, promote := range []bool{false, true} {
			prog, _ := compile([]source{{p.name, p.src}})
			if promote {
				if err := promoteProgram(prog, &promoteStats{}); err != nil {
					t.Fatalf("%s: promotion: %v", p.name, err)
				}
			}
			asm, err := amd64.Emit(prog)
			if err != nil {
				t.Fatalf("%s: %v", p.name, err)
			}
			exe := filepath.Join(dir, fmt.Sprintf("%s.%d", strings.TrimSuffix(p.name, ".go"), i))
			if ds, err := assemble(asm, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
				t.Fatalf("%s: not built: %v %v", p.name, ds, err)
			}
			if got := runExe(t, exe); got != want {
				t.Errorf("%s: native (SSA %v) %+v, interpreter %+v", p.name, promote, got, want)
			}
			code[i], stack[i] = measureAsm(t, asm)
			if promote {
				// Only a cycle of phi copies writes a cell from %rcx.
				cycles += strings.Count(string(asm), "\tmovq\t%rcx, -")
			}
			for name, n := range funcBytes(t, exe) {
				c, ok := code[i][name]
				if !ok {
					t.Fatalf("%s: symbol %s is not a function of the assembly", p.name, name)
				}
				c.bytes = n
				code[i][name] = c
			}
		}
		slot.mainStack += stack[0]
		ssa.mainStack += stack[1]
		for _, name := range slices.Sorted(maps.Keys(code[0])) {
			s, q := code[0][name], code[1][name]
			slot.add(s)
			ssa.add(q)
			fmt.Fprintf(&report, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n", p.name, name, s.instrs, q.instrs, s.bytes, q.bytes, s.frame, q.frame)
			for metric, worse := range map[string]bool{"instrs": q.instrs > s.instrs, "bytes": q.bytes > s.bytes, "frame": q.frame > s.frame} {
				if worse {
					regress[metric]++
				}
			}
		}
	}
	t.Logf("%d programs (%d corpus, %d generated, %d overflow) built and run both ways; all match the interpreter",
		len(progs), corpusRun, generatedPrograms, len(overflowPrograms))
	t.Logf("functions: %d slot, %d SSA", slot.funcs, ssa.funcs)
	t.Logf("machine instructions: slot %d, SSA %d (%s)", slot.instrs, ssa.instrs, ratio(ssa.instrs, slot.instrs))
	t.Logf("code bytes: slot %d, SSA %d (%s)", slot.bytes, ssa.bytes, ratio(ssa.bytes, slot.bytes))
	t.Logf("frame bytes, summed over functions: slot %d, SSA %d (%s); largest frame: slot %d, SSA %d",
		slot.frame, ssa.frame, ratio(ssa.frame, slot.frame), slot.maxFrame, ssa.maxFrame)
	t.Logf("aether.mainstack, summed over programs: slot %d, SSA %d (%s)", slot.mainStack, ssa.mainStack, ratio(ssa.mainStack, slot.mainStack))
	t.Logf("functions where SSA is larger: %d in instructions, %d in bytes, %d in frame size",
		regress["instrs"], regress["bytes"], regress["frame"])
	t.Logf("cycles of phi copies broken through %%rcx: %d", cycles)
	if path := os.Getenv("AETHER_SSA_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(report.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ratio formats x/y as a percentage change.
func ratio(x, y int) string {
	if y == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.1f%%", 100*float64(x-y)/float64(y))
}

// TestMeasureAsm checks the assembly measurement on a fixed function.
func TestMeasureAsm(t *testing.T) {
	p := &ir.Program{}
	at := ir.Pos{Line: 1, Col: 6}
	f := p.NewFunc("main", "p.go", at)
	f.Println(f.Entry(), []*ir.Instr{f.Const(f.Entry(), 1, at)}, at)
	f.Ret(f.Entry(), at)
	asm, err := amd64.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	funcs, stack := measureAsm(t, asm)
	// The stack check (4), push and move of %rbp (2), subq (1), const (1),
	// println (3), ret (3); main.init has only ret.
	want := map[string]funcCode{"main.main": {instrs: 14, frame: 16}, "main.init": {instrs: 1}}
	if !maps.Equal(funcs, want) || stack != 32 {
		t.Errorf("got %v, stack %d; want %v, stack 32", funcs, stack, want)
	}
}
