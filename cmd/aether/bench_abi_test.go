package main

// Calling-convention model for the benchmark harness (BENCHMARKS.md). It
// rewrites the assembly of aether build into candidate conventions, so they
// can be counted and timed without changing the compiler: the measurement
// behind benchmarks/2026-11-06-abi/REPORT.md. Nothing here reaches a built
// executable. The conventions are in abiVariants; nohop stays on ABI-S0, and
// abir1 is not rewritten: it is the compiler's own ABI-R1 (D-030).

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/ir"
)

var (
	modelArgs = []string{"%rdi", "%rsi", "%r8", "%r9", "%r10", "%r11"}
	modelCS   = []string{"%rbx", "%r12", "%r13", "%r14", "%r15"}

	regFamilies = [][]string{
		{"%rbx", "%ebx", "%bx", "%bl"}, {"%rsi", "%esi", "%si", "%sil"}, {"%rdi", "%edi", "%di", "%dil"},
		{"%r8", "%r8d", "%r8w", "%r8b"}, {"%r9", "%r9d", "%r9w", "%r9b"}, {"%r10", "%r10d", "%r10w", "%r10b"},
		{"%r11", "%r11d", "%r11w", "%r11b"}, {"%r12", "%r12d", "%r12w", "%r12b"}, {"%r13", "%r13d", "%r13w", "%r13b"},
		{"%r14", "%r14d", "%r14w", "%r14b"}, {"%r15", "%r15d", "%r15w", "%r15b"},
	}
	reRegTok  = regexp.MustCompile(`%[a-z0-9]+`)
	reCell    = regexp.MustCompile(`-(\d+)\(%rbp\)`)
	reArgIn   = regexp.MustCompile(`(^|[ \t,])(\d+)\(%rbp\)`)
	reArgOut  = regexp.MustCompile(`(\d+)\(%rsp\)`)
	reMove    = regexp.MustCompile(`^\t(movq|movl|movabsq)\t(.+), (.+)$`)
	abiLayout = []int{0, 8, 16, 24, 32, 40, 48, 56}
)

// abiVariants are the conventions of the model. isel is the shipped build;
// h_r1 and h_cs replace fib and ack with hand-written code. abir1 is not a
// model: it is ABI-R1 (D-030) as the compiler emits it, which is what aether
// build produced from D-031 until the register forms of arith.go; two adds them,
// and is aether build. isel stays the ABI-S0 build the models rewrite.
var abiVariants = []string{"isel", "nohop", "r1", "r1leaf", "r1cs", "h_r1", "h_cs", "abir1", "two"}

// abiSelected returns vs, or only those named in AETHER_ABI_VARIANTS
// (comma-separated) if it is set: TestABITime then times just those.
func abiSelected(t *testing.T, vs []string) []string {
	sel := os.Getenv("AETHER_ABI_VARIANTS")
	if sel == "" {
		return vs
	}
	names := strings.Split(sel, ",")
	for _, n := range names {
		if !slices.Contains(vs, n) {
			t.Fatalf("AETHER_ABI_VARIANTS: no variant %q", n)
		}
	}
	return slices.DeleteFunc(slices.Clone(vs), func(v string) bool { return !slices.Contains(names, v) })
}

// abiAsm returns a workload's program and its assembly as variant: ra and
// abir1 and two as the compiler emits them, the others rewritten from isel.
func abiAsm(t *testing.T, name string, src []byte, variant string) (*ir.Program, []byte) {
	t.Helper()
	if variant == "ra" || variant == "abir1" || variant == "two" {
		return benchEmit(t, name, src, variant)
	}
	prog, asm := benchEmit(t, name, src, "isel")
	re, err := abiRewrite(asm, prog, variant)
	if err != nil {
		t.Fatalf("%s %s: %v", name, variant, err)
	}
	return prog, re
}

type abiSig struct{ p, r int }

func abiSigs(prog *ir.Program) map[string]abiSig {
	m := map[string]abiSig{}
	for _, f := range prog.Funcs {
		m["main."+f.Name] = abiSig{len(f.Params), len(f.Results)}
	}
	return m
}

func asmInstr(l string) bool {
	return strings.HasPrefix(l, "\t") && !strings.HasPrefix(l, "\t#") && !strings.HasPrefix(l, "\t.")
}
func opReg(s string) bool { return strings.HasPrefix(s, "%") }
func opImm(s string) bool { return strings.HasPrefix(s, "$") }

// offsetOf parses op as N(base).
func offsetOf(op, base string) (int, bool) {
	s, ok := strings.CutSuffix(op, "("+base+")")
	if !ok || strings.HasPrefix(s, "-") {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// nextInstr returns the index of the first instruction at or after i.
func nextInstr(lines []string, i int) int {
	for !asmInstr(lines[i]) {
		i++
	}
	return i
}

// mapFuncs applies fn to the lines of each function, .type through .size.
func mapFuncs(asm []byte, fn func(name string, lines []string) ([]string, error)) ([]byte, error) {
	var out, body []string
	var cur string
	for _, l := range strings.Split(string(asm), "\n") {
		if cur == "" {
			if m := funcType.FindStringSubmatch(l); m != nil {
				cur, body = m[1], []string{l}
			} else {
				out = append(out, l)
			}
			continue
		}
		body = append(body, l)
		if strings.HasPrefix(l, "\t.size\t") {
			nb, err := fn(cur, body)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", cur, err)
			}
			out = append(out, nb...)
			cur = ""
		}
	}
	return []byte(strings.Join(out, "\n")), nil
}

type parMove struct{ src, dst string }

func moveTo(src, dst string) []string {
	if opImm(src) && opReg(dst) {
		if c, err := strconv.ParseInt(src[1:], 10, 64); err == nil && !fitsInt32(c) {
			return []string{fmt.Sprintf("\tmovabsq\t%s, %s", src, dst)}
		}
	}
	if !opReg(src) && !opImm(src) && !opReg(dst) {
		return []string{fmt.Sprintf("\tmovq\t%s, %%rax", src), fmt.Sprintf("\tmovq\t%%rax, %s", dst)}
	}
	return []string{fmt.Sprintf("\tmovq\t%s, %s", src, dst)}
}

func fitsInt32(c int64) bool { return c >= -1<<31 && c < 1<<31 }

// parallelCopy sequences a parallel copy, with %rax as the temporary. A
// memory destination is never a source, so a cycle holds only registers.
func parallelCopy(ms []parMove) []string {
	var out []string
	var p []parMove
	for _, m := range ms {
		if m.src != m.dst {
			p = append(p, m)
		}
	}
	for len(p) > 0 {
		i := slices.IndexFunc(p, func(m parMove) bool {
			return !slices.ContainsFunc(p, func(o parMove) bool { return o != m && o.src == m.dst })
		})
		if i >= 0 {
			out = append(out, moveTo(p[i].src, p[i].dst)...)
			p = slices.Delete(p, i, i+1)
			continue
		}
		s := p[0].src
		out = append(out, fmt.Sprintf("\tmovq\t%s, %%rax", s))
		for j := range p {
			if p[j].src == s {
				p[j].src = "%rax"
			}
		}
	}
	return out
}

// popRAXLoad removes the last instruction of out, which must load %rax, and
// returns its source.
func popRAXLoad(out []string) ([]string, string, error) {
	for i := len(out) - 1; i >= 0; i-- {
		if !asmInstr(out[i]) {
			continue
		}
		if out[i] == "\txorl\t%eax, %eax" {
			return slices.Delete(out, i, i+1), "$0", nil
		}
		m := reMove.FindStringSubmatch(out[i])
		if m == nil || (m[3] != "%rax" && m[3] != "%eax") {
			return nil, "", fmt.Errorf("no load of %%rax before an argument store: %q", out[i])
		}
		return slices.Delete(out, i, i+1), m[2], nil
	}
	return nil, "", fmt.Errorf("no load of %%rax")
}

// modelR1 rewrites one function: arguments 0-5 in modelArgs, a single result
// in %rax, everything else in its ABI-S0 position.
func modelR1(name string, lines []string, s map[string]abiSig) ([]string, error) {
	lines = slices.Clone(lines)
	me := s[name]
	var out []string
	var params, args []parMove
	flush := func() {
		out = append(out, parallelCopy(params)...)
		params = nil
	}
	outgoing := 0
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "\t# ") && !strings.Contains(l, "= param ") {
			flush()
		}
		if !asmInstr(l) {
			out = append(out, l)
			continue
		}
		f := strings.Fields(l)
		if f[0] == "call" {
			flush()
			c, ok := s[f[1]]
			if !ok {
				if len(args) > 0 {
					return nil, fmt.Errorf("arguments before helper %s", f[1])
				}
				out = append(out, l)
				continue
			}
			out = append(out, parallelCopy(args)...)
			out = append(out, l)
			args = nil
			if c.p > 6 || c.r > 1 {
				outgoing = max(outgoing, 8*(c.p+c.r))
			}
			if c.r == 1 {
				j := nextInstr(lines, i+1)
				if want := fmt.Sprintf("\tmovq\t%d(%%rsp), %%rax", 8*c.p); lines[j] != want {
					return nil, fmt.Errorf("no result load after a call: %q", lines[j])
				}
				lines = slices.Delete(lines, j, j+1)
			}
			continue
		}
		if m := reMove.FindStringSubmatch(l); m != nil {
			src, dst := m[2], m[3]
			if k, ok := offsetOf(src, "%rbp"); ok && k < 16+8*me.p && dst == "%rax" {
				j := nextInstr(lines, i+1)
				n := reMove.FindStringSubmatch(lines[j])
				if n == nil || n[2] != "%rax" {
					return nil, fmt.Errorf("a parameter load without a store: %q", lines[j])
				}
				if idx := (k - 16) / 8; idx < 6 {
					src = modelArgs[idx]
				}
				params = append(params, parMove{src, n[3]})
				out = append(out, lines[i+1:j]...)
				i = j
				continue
			}
			if k, ok := offsetOf(dst, "%rsp"); ok {
				if src == "%rax" {
					var err error
					if out, src, err = popRAXLoad(out); err != nil {
						return nil, err
					}
				}
				if k/8 < 6 {
					dst = modelArgs[k/8]
				}
				args = append(args, parMove{src, dst})
				continue
			}
			if k, ok := offsetOf(dst, "%rbp"); ok && k >= 16+8*me.p && me.r == 1 {
				if src != "%rax" {
					out = append(out, moveTo(src, "%rax")...)
				}
				continue
			}
		}
		out = append(out, l)
	}
	cells := 0
	for _, l := range out {
		if asmInstr(l) {
			for _, m := range reCell.FindAllStringSubmatch(l, -1) {
				n, _ := strconv.Atoi(m[1])
				cells = max(cells, n)
			}
		}
	}
	return setFrame(out, (cells+outgoing+15)/16*16)
}

// hasPrologue reports whether a function has one; main.init may be only ret.
func hasPrologue(lines []string) bool {
	return slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "aether.stacklimit") })
}

// setFrame rewrites the prologue for a frame of the given size.
func setFrame(lines []string, size int) ([]string, error) {
	if !hasPrologue(lines) {
		return lines, nil
	}
	var out []string
	added, done := false, false
	for k, l := range lines {
		switch {
		case !added && strings.HasPrefix(l, "\taddq\t$") && strings.HasSuffix(l, ", %rax"):
			out = append(out, fmt.Sprintf("\taddq\t$%d, %%rax", size+8))
			added = true
			continue
		case !done && frameAlloc.MatchString(l):
			if size > 0 {
				out = append(out, fmt.Sprintf("\tsubq\t$%d, %%rsp", size))
			}
			done = true
			continue
		}
		out = append(out, l)
		if !done && l == "\tmovq\t%rsp, %rbp" && !frameAlloc.MatchString(lines[k+1]) {
			if size > 0 {
				out = append(out, fmt.Sprintf("\tsubq\t$%d, %%rsp", size))
			}
			done = true
		}
	}
	if !added || !done {
		return nil, fmt.Errorf("prologue not found")
	}
	return out, nil
}

// checkModelClean rejects leftovers of stack transfer where R1 has none.
func checkModelClean(name string, lines []string, s map[string]abiSig) error {
	me := s[name]
	stackOK := false
	for _, l := range lines {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "call" {
			if c, ok := s[f[1]]; ok && (c.p > 6 || c.r > 1) {
				stackOK = true
			}
		}
	}
	for _, l := range lines {
		if !asmInstr(l) {
			continue
		}
		if reArgOut.MatchString(l) && !stackOK {
			return fmt.Errorf("leftover outgoing access: %q", l)
		}
		if reArgIn.MatchString(l) && me.p <= 6 && me.r <= 1 {
			return fmt.Errorf("leftover incoming access: %q", l)
		}
	}
	return nil
}

// modelLeaf removes the check and frame of a function with no frame use and
// no call except to a panic helper. It would amend D-011's frame chain.
func modelLeaf(lines []string) []string {
	if !hasPrologue(lines) {
		return lines
	}
	for _, l := range lines {
		if !asmInstr(l) {
			continue
		}
		if strings.Contains(l, "(%rbp)") || strings.Contains(l, "(%rsp)") || frameAlloc.MatchString(l) {
			return lines
		}
		if f := strings.Fields(l); f[0] == "call" && !strings.HasPrefix(f[1], "aether.panic") {
			return lines
		}
	}
	var out []string
	n := 0
	for _, l := range lines {
		if asmInstr(l) {
			n++
			// The check (4), pushq, and movq; then the epilogue's restores.
			if n <= 6 || l == "\tmovq\t%rbp, %rsp" || l == "\tpopq\t%rbp" {
				continue
			}
		}
		out = append(out, l)
	}
	return out
}

// modelNoHop stays on ABI-S0 and drops the %rax hop between a register and
// an ABI word.
func modelNoHop(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		m := reMove.FindStringSubmatch(l)
		if m == nil || m[1] != "movq" || m[3] != "%rax" {
			out = append(out, l)
			continue
		}
		j := i + 1
		for j < len(lines) && !asmInstr(lines[j]) {
			j++
		}
		n := reMove.FindStringSubmatch(lines[j])
		if n == nil || n[1] != "movq" || n[2] != "%rax" {
			out = append(out, l)
			continue
		}
		abiWord := func(op string) bool {
			_, in := offsetOf(op, "%rbp")
			_, ou := offsetOf(op, "%rsp")
			return in || ou
		}
		if opReg(m[2]) && abiWord(n[3]) || abiWord(m[2]) && opReg(n[3]) {
			out = append(out, lines[i+1:j]...)
			out = append(out, fmt.Sprintf("\tmovq\t%s, %s", m[2], n[3]))
			i = j
			continue
		}
		out = append(out, l)
	}
	return out
}

func regFamily(r string) []string {
	return regFamilies[slices.IndexFunc(regFamilies, func(f []string) bool { return f[0] == r })]
}

func usesReg(lines []string, r string) bool {
	fam := regFamily(r)
	return slices.ContainsFunc(lines, func(l string) bool {
		return asmInstr(l) && slices.ContainsFunc(reRegTok.FindAllString(l, -1), func(t string) bool { return slices.Contains(fam, t) })
	})
}

// modelCalleeSaved moves the most-used cells of a function that calls into
// modelCS, renames temporaries out of those registers where a caller-saved
// one is free, and saves in the prologue, on every path, whichever of them
// the function still uses. Run it before modelR1, whose copies handle any
// register assignment.
func modelCalleeSaved(lines []string) []string {
	calls := slices.ContainsFunc(lines, func(l string) bool {
		f := strings.Fields(l)
		return len(f) == 2 && f[0] == "call" && !strings.HasPrefix(f[1], "aether.panic")
	})
	var free, kept, avail, used []string
	for _, r := range []string{"%rsi", "%rdi", "%r8", "%r9", "%r10", "%r11"} {
		if !usesReg(lines, r) {
			free = append(free, r)
		}
	}
	rename := map[string]string{}
	for _, r := range modelCS {
		switch {
		case !usesReg(lines, r):
			avail = append(avail, r)
		case len(free) > 0:
			fa, fb := regFamily(r), regFamily(free[0])
			for i := range fa {
				rename[fa[i]] = fb[i]
			}
			free = free[1:]
			avail = append(avail, r)
		default:
			kept = append(kept, r)
		}
	}
	count := map[string]int{}
	for _, l := range lines {
		if asmInstr(l) {
			for _, m := range reCell.FindAllString(l, -1) {
				count[m]++
			}
		}
	}
	cells := slices.Sorted(maps.Keys(count))
	sort.SliceStable(cells, func(i, j int) bool { return count[cells[i]] > count[cells[j]] })
	cellMap := map[string]string{}
	used = append(used, kept...)
	next := 0
	for _, c := range cells {
		if calls && len(avail) > 0 {
			cellMap[c] = avail[0]
			used = append(used, avail[0])
			avail = avail[1:]
		} else {
			next += 8
			cellMap[c] = fmt.Sprintf("-%d(%%rbp)", next)
		}
	}
	slices.Sort(used)
	saves := map[string]string{}
	for _, r := range used {
		next += 8
		saves[r] = fmt.Sprintf("-%d(%%rbp)", next)
	}
	var out []string
	placed := false
	for _, l := range lines {
		if !placed && reBlock.MatchString(l) {
			for _, r := range used {
				out = append(out, fmt.Sprintf("\tmovq\t%s, %s", r, saves[r]))
			}
			placed = true
		}
		if asmInstr(l) {
			l = reRegTok.ReplaceAllStringFunc(l, func(t string) string {
				if n, ok := rename[t]; ok {
					return n
				}
				return t
			})
			l = reCell.ReplaceAllStringFunc(l, func(c string) string { return cellMap[c] })
			if l == "\tmovq\t%rbp, %rsp" {
				for _, r := range used {
					out = append(out, fmt.Sprintf("\tmovq\t%s, %s", saves[r], r))
				}
			}
		}
		out = append(out, l)
	}
	return out
}

// handFunc assembles a hand-written function with the prologue of a 16-byte
// frame. RET in body is the epilogue.
func handFunc(name, body string) string {
	sym := "main." + name
	head := "\t.type\t" + sym + ", @function\n" + sym + ":\n" +
		"\tmovq\taether.stacklimit(%rip), %rax\n\taddq\t$24, %rax\n\tcmpq\t%rax, %rsp\n\tjb\taether.stackoverflow\n" +
		"\tpushq\t%rbp\n\tmovq\t%rsp, %rbp\n\tsubq\t$16, %rsp\n"
	body = strings.ReplaceAll(body, "RET\n", "\tmovq\t%rbp, %rsp\n\tpopq\t%rbp\n\tret\n")
	return head + body + "\t.size\t" + sym + ", ." + "-" + sym
}

const handFibBase = ".L0.b0:\n\tcmpq\t$2, %rdi\n\tjge\t.L0.b2\n.L0.b1:\n\tmovq\t%rdi, %rax\nRET\n.L0.b2:\n"

const handAckBase = ".L0.b0:\n\ttestq\t%rdi, %rdi\n\tjne\t.L0.b2\n.L0.b1:\n\tleaq\t1(%rsi), %rax\nRET\n" +
	".L0.b2:\n\ttestq\t%rsi, %rsi\n\tjne\t.L0.b4\n.L0.b3:\n\tsubq\t$1, %rdi\n\tmovl\t$1, %esi\n\tcall\tmain.ack\nRET\n.L0.b4:\n"

// handFuncs are the hand-written functions: h_r1 is r1 with coalescing, and
// h_cs is callee-saved with saves only on recursive paths.
var handFuncs = map[string]map[string]string{
	"h_r1": {
		"main.fib": handFunc("fib", handFibBase+"\tmovq\t%rdi, -8(%rbp)\n\tsubq\t$1, %rdi\n\tcall\tmain.fib\n\tmovq\t%rax, -16(%rbp)\n"+
			"\tmovq\t-8(%rbp), %rdi\n\tsubq\t$2, %rdi\n\tcall\tmain.fib\n\taddq\t-16(%rbp), %rax\nRET\n"),
		"main.ack": handFunc("ack", handAckBase+"\tleaq\t-1(%rdi), %rax\n\tmovq\t%rax, -8(%rbp)\n\tsubq\t$1, %rsi\n\tcall\tmain.ack\n"+
			"\tmovq\t%rax, %rsi\n\tmovq\t-8(%rbp), %rdi\n\tcall\tmain.ack\nRET\n"),
	},
	"h_cs": {
		"main.fib": handFunc("fib", handFibBase+"\tmovq\t%rbx, -8(%rbp)\n\tmovq\t%r12, -16(%rbp)\n\tmovq\t%rdi, %rbx\n\tsubq\t$1, %rdi\n\tcall\tmain.fib\n"+
			"\tmovq\t%rax, %r12\n\tleaq\t-2(%rbx), %rdi\n\tcall\tmain.fib\n\taddq\t%r12, %rax\n\tmovq\t-8(%rbp), %rbx\n\tmovq\t-16(%rbp), %r12\nRET\n"),
		"main.ack": handFunc("ack", handAckBase+"\tmovq\t%rbx, -8(%rbp)\n\tleaq\t-1(%rdi), %rbx\n\tsubq\t$1, %rsi\n\tcall\tmain.ack\n"+
			"\tmovq\t%rax, %rsi\n\tmovq\t%rbx, %rdi\n\tcall\tmain.ack\n\tmovq\t-8(%rbp), %rbx\nRET\n"),
	},
}

// abiRewrite turns asm, emitted for prog by aether build, into variant.
func abiRewrite(asm []byte, prog *ir.Program, variant string) ([]byte, error) {
	s := abiSigs(prog)
	switch variant {
	case "isel":
		return asm, nil
	case "nohop":
		return mapFuncs(asm, func(_ string, l []string) ([]string, error) { return modelNoHop(l), nil })
	}
	return mapFuncs(asm, func(n string, l []string) ([]string, error) {
		if text, ok := handFuncs[variant][n]; ok {
			return strings.Split(text, "\n"), nil
		}
		if variant == "r1cs" {
			l = modelCalleeSaved(l)
		}
		o, err := modelR1(n, l, s)
		if err != nil {
			return nil, err
		}
		if err := checkModelClean(n, o, s); err != nil {
			return nil, err
		}
		if variant == "r1leaf" {
			o = modelLeaf(o)
		}
		return o, nil
	})
}

// abiApplies reports whether variant exists for a workload: the hand-written
// ones only for fib and ack.
func abiApplies(w, v string) bool {
	return !strings.HasPrefix(v, "h_") || w == "b01_fib" || w == "b08_ackermann"
}

// abiPad shifts the program's text by k bytes from a 64-byte boundary.
func abiPad(asm []byte, k int) []byte {
	s := string(asm)
	i := strings.Index(s, "\t.text\n")
	return []byte(s[:i] + fmt.Sprintf("\t.text\n\t.p2align\t6\n\t.skip\t%d\n", k) + s[i+len("\t.text\n"):])
}

// TestABICounts builds, checks, and profiles every variant: abi_summary.tsv.
func TestABICounts(t *testing.T) {
	dir := benchOut(t)
	var sum strings.Builder
	sum.WriteString("workload\tvariant\tdyn instrs\tdyn mem\tcell\tabi mem\treg-reg\tcalls\tpeak stack\ttext bytes\tstatic instrs\tframe bytes\n")
	for _, w := range benchWorkloads(t) {
		for _, v := range abiVariants {
			if !abiApplies(w.name, v) {
				continue
			}
			prog, re := abiAsm(t, w.name+".go", w.src, v)
			exe := filepath.Join(dir, w.name+"."+v)
			if err := os.WriteFile(exe+".s", re, 0o644); err != nil {
				t.Fatal(err)
			}
			if ds, err := assemble(re, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
				t.Fatalf("%s %s: %v %v", w.name, v, ds, err)
			}
			_, stderr, code, _, _ := benchRun(t, exe)
			if err := checkRun(stderr, code, w.want); err != nil {
				t.Fatalf("%s %s: %v", w.name, v, err)
			}
			d, _, _ := profile(t, prog, re, exe+".prof", w.want)
			static, _ := measureAsm(t, re)
			var si, sf, sb int
			for _, c := range static {
				si += c.instrs
				sf += c.frame
			}
			for _, n := range funcBytes(t, exe) {
				sb += n
			}
			fmt.Fprintf(&sum, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", w.name, v, d.instrs, d.mem(), d.cell(), d.abi(), d.movRR, d.calls, d.peakStack, sb, si, sf)
		}
	}
	writeReport(t, dir, "abi_summary.tsv", sum.String())
}

// abiCategory classifies an instruction of the shipped build. n counts the
// function's instructions so far, and afterCall says a call of the same IR
// instruction came before it.
func abiCategory(line, irOp string, n int, afterCall bool) string {
	switch {
	case irOp == "prologue" && n <= 4:
		return "check"
	case irOp == "prologue":
		return "frame"
	case irOp == "param":
		return "param"
	case irOp == "call" && strings.HasPrefix(line, "\tcall\t"):
		return "call"
	case irOp == "call" && !afterCall:
		return "argxfer"
	case irOp == "call":
		return "resultpick"
	case irOp == "ret" && (line == "\tmovq\t%rbp, %rsp" || line == "\tpopq\t%rbp" || line == "\tret"):
		return "teardown"
	case irOp == "ret":
		return "retwrite"
	case reCell.MatchString(line):
		return "cell-other"
	}
	return "body"
}

// TestABIAttribution splits the dynamic instructions and memory operations of
// the shipped build into call-related categories: abi_attr.tsv, abi_attr_mem.tsv.
func TestABIAttribution(t *testing.T) {
	dir := benchOut(t)
	cats := []string{"check", "frame", "argxfer", "call", "resultpick", "param", "retwrite", "teardown", "cell-other", "body"}
	var b, mb strings.Builder
	b.WriteString("workload\ttotal\tcalls\t" + strings.Join(cats, "\t") + "\n")
	mb.WriteString("workload\ttotal mem\t" + strings.Join(cats, "\t") + "\n")
	for _, w := range benchWorkloads(t) {
		prog, asm := benchEmit(t, w.name+".go", w.src, "isel")
		d, _, counts := profile(t, prog, asm, filepath.Join(dir, w.name+".attr"), w.want)
		tot, mem := map[string]int64{}, map[string]int64{}
		open, region, n := -1, -1, 0
		irOp, inFn, afterCall := "", false, false
		// The same walk as instrument, so that region numbers agree.
		for _, line := range strings.Split(string(asm), "\n") {
			switch {
			case funcType.MatchString(line):
				irOp, inFn, n = "prologue", true, 0
				continue
			case !inFn:
				continue
			case strings.HasPrefix(line, "\t# "):
				f := strings.Fields(line)
				irOp = f[1]
				if len(f) > 3 && f[2] == "=" {
					irOp = f[3]
				}
				afterCall = false
				continue
			case strings.HasPrefix(line, "\t.size\t"):
				inFn, open = false, -1
				continue
			case reLabel.MatchString(line):
				region++
				open = region
				continue
			case !isInstr(line):
				continue
			}
			n++
			f := strings.Fields(line)
			cat := abiCategory(line, irOp, n, afterCall)
			if cat == "call" {
				afterCall = true
			}
			tot[cat] += counts[open]
			if strings.Contains(line, "(%r") && f[0] != "leaq" || slices.Contains([]string{"pushq", "popq", "call", "ret"}, f[0]) {
				mem[cat] += counts[open]
			}
			if f[0][0] == 'j' && f[0] != "jmp" {
				region++
				open = region
			}
		}
		var sum, msum int64
		fmt.Fprintf(&b, "%s\t%d\t%d", w.name, d.instrs, d.calls)
		fmt.Fprintf(&mb, "%s\t%d", w.name, d.mem())
		for _, c := range cats {
			sum += tot[c]
			msum += mem[c]
			fmt.Fprintf(&b, "\t%d", tot[c])
			fmt.Fprintf(&mb, "\t%d", mem[c])
		}
		b.WriteString("\n")
		mb.WriteString("\n")
		if sum != d.instrs || msum != d.mem() {
			t.Errorf("%s: categories sum to %d of %d instrs, %d of %d mem", w.name, sum, d.instrs, msum, d.mem())
		}
	}
	writeReport(t, dir, "abi_attr.tsv", b.String())
	writeReport(t, dir, "abi_attr_mem.tsv", mb.String())
}

// TestABIArity reports the suite's signatures and call sites: abi_arity.tsv.
func TestABIArity(t *testing.T) {
	dir := benchOut(t)
	progs, _ := s2Suite(t)
	fnP, fnR, siteP, siteR, across := map[int]int{}, map[int]int{}, map[int]int{}, map[int]int{}, map[int]int{}
	nest, calls := 0, 0
	for _, p := range progs {
		prog, ds := compile([]source{{p.name, p.src}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if err := promoteProgram(prog, &promoteStats{}); err != nil {
			t.Fatal(err)
		}
		for _, f := range prog.Funcs {
			if f.Name != "init" && f.Name != "main" {
				fnP[len(f.Params)]++
				fnR[len(f.Results)]++
			}
			across[analyze(f, nil).maxAcrossCall]++
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					if in.Op != ir.Call {
						continue
					}
					calls++
					siteP[len(in.Callee.Params)]++
					siteR[len(in.Callee.Results)]++
					for _, a := range in.Args {
						if a.Op == ir.Call {
							nest++
						}
					}
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "programs\t%d\ncall sites\t%d\ncall sites with a call result as argument\t%d\n", len(progs), calls, nest)
	for _, d := range []struct {
		name string
		m    map[int]int
	}{{"functions by params", fnP}, {"functions by results", fnR}, {"sites by params", siteP}, {"sites by results", siteR}, {"functions by max across one call", across}} {
		for _, k := range slices.Sorted(maps.Keys(d.m)) {
			fmt.Fprintf(&b, "%s\t%d\t%d\n", d.name, k, d.m[k])
		}
	}
	writeReport(t, dir, "abi_arity.tsv", b.String())
}

// abiRunAgainstInterp rewrites src to variant, builds it into dir, and
// compares it with the interpreter.
func abiRunAgainstInterp(t *testing.T, dir, name string, src []byte, variant string) error {
	t.Helper()
	want, ds, err := interpret(name, src)
	if len(ds) > 0 || err != nil {
		t.Fatalf("%s: %v %v", name, ds, err)
	}
	prog, ds := compile([]source{{name, src}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	compiled := variant == "abir1" || variant == "two"
	opts := amd64.EmitOptions{Registers: true, Select: true, ABIR1: compiled, TwoAddress: variant == "two"}
	asm, err := emitProgramWith(prog, opts)
	if err != nil {
		return fmt.Errorf("emit: %v", err)
	}
	re := asm
	if !compiled {
		if re, err = abiRewrite(asm, prog, variant); err != nil {
			return fmt.Errorf("rewrite: %v", err)
		}
	}
	exe := filepath.Join(dir, "abi")
	if ds, err := assemble(re, buildOptions{out: exe}, io.Discard); len(ds) > 0 || err != nil {
		return fmt.Errorf("build: %v %v", ds, err)
	}
	if got := runExe(t, exe); got != want {
		return fmt.Errorf("got %+v, want %+v", got, want)
	}
	return nil
}

// TestABISuite runs each model on the S2 suite against the interpreter:
// abi_suite.txt.
func TestABISuite(t *testing.T) {
	dir := benchOut(t)
	progs, _ := s2Suite(t)
	work := t.TempDir()
	var rep strings.Builder
	for _, v := range []string{"nohop", "r1", "r1leaf", "r1cs", "abir1", "two"} {
		bad := 0
		for _, p := range progs {
			if err := abiRunAgainstInterp(t, work, p.name, p.src, v); err != nil {
				t.Errorf("%s %s: %v", v, p.name, err)
				bad++
			}
		}
		fmt.Fprintf(&rep, "%s\tprograms %d\tran %d\tmismatches or failures %d\n", v, len(progs), len(progs), bad)
	}
	writeReport(t, dir, "abi_suite.txt", rep.String())
}

type abiTimeKey struct {
	w, v string
	off  int
}

// abiParseTimes reads TestABITime's raw output: sorted CPU ms by workload,
// variant, and offset, in first-seen order.
func abiParseTimes(raw string) (map[abiTimeKey][]float64, []abiTimeKey, error) {
	times := map[abiTimeKey][]float64{}
	var order []abiTimeKey
	for _, l := range strings.Split(strings.TrimSpace(raw), "\n")[1:] {
		f := strings.Split(l, "\t")
		if len(f) != 5 {
			return nil, nil, fmt.Errorf("bad line %q", l)
		}
		off, err1 := strconv.Atoi(f[2])
		ns, err2 := strconv.ParseInt(f[4], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, nil, fmt.Errorf("bad line %q", l)
		}
		k := abiTimeKey{f[0], f[1], off}
		if _, ok := times[k]; !ok {
			order = append(order, k)
		}
		times[k] = append(times[k], float64(ns)/1e6)
	}
	for _, x := range times {
		slices.Sort(x)
	}
	return times, order, nil
}

// abiTimesSummary summarizes the timing at the shipped layout.
func abiTimesSummary(raw string) (string, error) {
	times, order, err := abiParseTimes(raw)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("workload\tvariant\tn\tcpu median ms\tcpu min\tcpu max\tratio to isel\n")
	for _, k := range order {
		x := times[k]
		m := median(x)
		fmt.Fprintf(&b, "%s\t%s\t%d\t%.1f\t%.1f\t%.1f\t%.3f\n", k.w, k.v, len(x), m, slices.Min(x), slices.Max(x), m/median(times[abiTimeKey{k.w, "isel", k.off}]))
	}
	return b.String(), nil
}

// abiLayoutSummary gives each variant's median at each offset, the median of
// those, and its ratio to isel's.
func abiLayoutSummary(raw string) (string, error) {
	times, _, err := abiParseTimes(raw)
	if err != nil {
		return "", err
	}
	medians := func(w, v string) ([]float64, float64) {
		var ms []float64
		for _, o := range abiLayout {
			ms = append(ms, median(times[abiTimeKey{w, v, o}]))
		}
		return ms, median(slices.Sorted(slices.Values(ms)))
	}
	var b strings.Builder
	b.WriteString("workload\tvariant")
	for _, o := range abiLayout {
		fmt.Fprintf(&b, "\toff%d", o)
	}
	b.WriteString("\tmedian of offsets\tratio to isel\n")
	seen := map[[2]string]bool{}
	for k := range times {
		seen[[2]string{k.w, k.v}] = true
	}
	keys := slices.SortedFunc(maps.Keys(seen), func(a, b [2]string) int { return slices.Compare(a[:], b[:]) })
	for _, k := range keys {
		ms, m := medians(k[0], k[1])
		_, base := medians(k[0], "isel")
		fmt.Fprintf(&b, "%s\t%s", k[0], k[1])
		for _, x := range ms {
			fmt.Fprintf(&b, "\t%.1f", x)
		}
		fmt.Fprintf(&b, "\t%.1f\t%.3f\n", m, m/base)
	}
	return b.String(), nil
}

// TestABITime times every variant and ra round-robin, with a discarded
// warm-up, as TestBenchTime does: at the shipped layout with gc, or with
// AETHER_ABI_LAYOUT set, at every offset of abiLayout.
func TestABITime(t *testing.T) {
	dir := benchOut(t)
	runs, err := strconv.Atoi(os.Getenv("AETHER_BENCH_RUNS"))
	if err != nil || runs < 2 {
		t.Fatalf("AETHER_BENCH_RUNS=%q: want an integer of at least 2", os.Getenv("AETHER_BENCH_RUNS"))
	}
	layout := os.Getenv("AETHER_ABI_LAYOUT") != ""
	offsets, file, summarize := []int{-1}, "abi_times", abiTimesSummary
	if layout {
		offsets, file, summarize = abiLayout, "abi_layout", abiLayoutSummary
	}
	var raw strings.Builder
	raw.WriteString("workload\tvariant\toffset\trun\tcpu ns\n")
	work := filepath.Join(dir, "layout")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, w := range benchWorkloads(t) {
		var exes []abiTimeKey
		path := map[abiTimeKey]string{}
		for _, v := range abiSelected(t, append([]string{"ra"}, abiVariants...)) {
			if !abiApplies(w.name, v) {
				continue
			}
			for _, off := range offsets {
				_, asm := abiAsm(t, w.name+".go", w.src, v)
				if off >= 0 {
					asm = abiPad(asm, off)
				}
				k := abiTimeKey{w.name, v, off}
				path[k] = filepath.Join(work, fmt.Sprintf("%s.%s.%d", w.name, v, off))
				if ds, err := assemble(asm, buildOptions{out: path[k]}, io.Discard); len(ds) > 0 || err != nil {
					t.Fatalf("%s: %v %v", path[k], ds, err)
				}
				exes = append(exes, k)
			}
		}
		if !layout {
			k := abiTimeKey{w.name, "gc", -1}
			path[k] = filepath.Join(dir, w.name+".gc")
			exes = append(exes, k)
		}
		for r := -1; r < runs; r++ {
			for _, k := range exes {
				_, stderr, code, _, cpu := benchRun(t, path[k])
				if err := checkRun(stderr, code, w.want); err != nil {
					t.Fatalf("%s: %v", path[k], err)
				}
				if r >= 0 {
					fmt.Fprintf(&raw, "%s\t%s\t%d\t%d\t%d\n", k.w, k.v, k.off, r, cpu.Nanoseconds())
				}
			}
		}
	}
	writeReport(t, dir, file+".tsv", raw.String())
	sum, err := summarize(raw.String())
	if err != nil {
		t.Fatal(err)
	}
	writeReport(t, dir, file+"_summary.tsv", sum)
}

// TestParallelCopy simulates the copies of every permutation of four
// registers, with memory and immediate sources: each destination must end up
// with its source's old value.
func TestParallelCopy(t *testing.T) {
	regs := []string{"%rdi", "%rsi", "%r8", "%r9"}
	check := func(p []string) {
		ms := []parMove{{"-8(%rbp)", "%r10"}, {"$5", "%r11"}, {"%rdi", "-16(%rbp)"}}
		state := map[string]string{"-8(%rbp)": "-8(%rbp)", "$5": "$5"}
		for i, r := range regs {
			ms = append(ms, parMove{p[i], r})
			state[r] = r
		}
		for _, l := range parallelCopy(ms) {
			m := reMove.FindStringSubmatch(l)
			state[m[3]] = state[m[2]]
		}
		for _, m := range ms {
			if state[m.dst] != m.src {
				t.Errorf("permutation %v: %s holds %s, want %s", p, m.dst, state[m.dst], m.src)
			}
		}
	}
	var walk func(p []string, k int)
	walk = func(p []string, k int) {
		if k == len(p) {
			check(p)
			return
		}
		for i := k; i < len(p); i++ {
			p[k], p[i] = p[i], p[k]
			walk(p, k+1)
			p[k], p[i] = p[i], p[k]
		}
	}
	walk(slices.Clone(regs), 0)
}

// TestABIModel runs each model on calls with more than six arguments, several
// results, and recursion, against the interpreter, and checks the summaries.
func TestABIModel(t *testing.T) {
	src := []byte(`package main

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func eight(a, b, c, d, e, f, g, h int) int { return a - b + c - d + e - f + g*h }

func two(x int) (int, int) { return x + 1, x * 2 }

func swap(a, b int) int { return eight(b, a, b, a, 7, 9223372036854775807, b, a) }

func main() {
	x, y := two(fib(10))
	println(x, y, swap(x, y), eight(1, 2, 3, 4, 5, 6, fib(5), 8))
}
`)
	work := t.TempDir()
	for _, v := range abiVariants {
		if err := abiRunAgainstInterp(t, work, "abi.go", src, v); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
	// Medians of unsorted runs at each offset, and of unsorted offset medians.
	raw := "workload\tvariant\toffset\trun\tcpu ns\n"
	for i, o := range abiLayout {
		raw += fmt.Sprintf("w\tisel\t%d\t0\t%d\nw\tisel\t%d\t1\t%d\nw\tisel\t%d\t2\t%d\n", o, 9000000, o, (8-i)*1000000, o, 1000000)
		raw += fmt.Sprintf("w\tr1\t%d\t0\t2000000\n", o)
	}
	got, err := abiLayoutSummary(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "w\tisel\t8.0\t7.0\t6.0\t5.0\t4.0\t3.0\t2.0\t1.0\t4.5\t1.000\n" +
		"w\tr1\t2.0\t2.0\t2.0\t2.0\t2.0\t2.0\t2.0\t2.0\t2.0\t0.444\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("layout summary:\n%s\nwant it to end with\n%s", got, want)
	}
	shipped, err := abiTimesSummary("workload\tvariant\toffset\trun\tcpu ns\nw\tisel\t-1\t0\t3000000\nw\tisel\t-1\t1\t1000000\nw\tisel\t-1\t2\t2000000\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "w\tisel\t3\t2.0\t1.0\t3.0\t1.000\n"; !strings.HasSuffix(shipped, want) {
		t.Errorf("times summary:\n%s\nwant it to end with %q", shipped, want)
	}
}
