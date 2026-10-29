package amd64

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// The tests in this file check the D-009 Phase 4 stack: its mapping, the
// overflow handler at the edge of the reserve, and frames larger than the
// stack.

const stackDefault = 256 << 20 // S for any program whose main needs less

// mappingHarness writes the stack top (the address above main.main's
// return address) and L to stdout as two little-endian words, then blocks
// reading one byte from stdin, so the test can inspect the live process.
var mappingHarness = fmt.Sprintf(`	.text
	.globl	main.main
main.main:
	leaq	8(%%rsp), %%rax
	subq	$24, %%rsp
	movq	%%rax, 0(%%rsp)
	movq	%s(%%rip), %%rax
	movq	%%rax, 8(%%rsp)
	movl	$1, %%eax		# write
	movl	$1, %%edi
	movq	%%rsp, %%rsi
	movl	$16, %%edx
	syscall
	xorl	%%eax, %%eax		# read
	xorl	%%edi, %%edi
	movq	%%rsp, %%rsi
	movl	$1, %%edx
	syscall
	addq	$24, %%rsp
	ret
`, symStackLimit) + mainStack(16)

// mapping is one entry of /proc/<pid>/smaps.
type mapping struct {
	start, end uint64
	perms      string
	flags      []string // VmFlags
}

func readSmaps(t *testing.T, pid int) []mapping {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/smaps", pid))
	if err != nil {
		t.Skipf("cannot read smaps: %v", err)
	}
	var ms []mapping
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if rest, ok := strings.CutPrefix(f[0], "VmFlags:"); ok && rest == "" && len(ms) > 0 {
			ms[len(ms)-1].flags = f[1:]
			continue
		}
		lo, hi, ok := strings.Cut(f[0], "-")
		if !ok || len(f) < 2 {
			continue
		}
		start, err1 := strconv.ParseUint(lo, 16, 64)
		end, err2 := strconv.ParseUint(hi, 16, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		ms = append(ms, mapping{start: start, end: end, perms: f[1]})
	}
	return ms
}

// containing returns the mapping that contains [lo, hi), or nil.
func containing(ms []mapping, lo, hi uint64) *mapping {
	for i := range ms {
		if ms[i].start <= lo && hi <= ms[i].end {
			return &ms[i]
		}
	}
	return nil
}

// TestStackMapping checks the D-009 layout in a live process: the stack
// top is L + S, [L - 4096, top) is mapped rw-p without MAP_NORESERVE (no nr
// in VmFlags), and the page [L - 8192, L - 4096) is a PROT_NONE guard. The
// kernel can merge adjacent mappings, so the checks use containment. Under
// overcommit mode 2 the kernel ignores MAP_NORESERVE and the nr check
// proves nothing; the mode is logged.
func TestStackMapping(t *testing.T) {
	if mode, err := os.ReadFile("/proc/sys/vm/overcommit_memory"); err == nil {
		t.Logf("vm.overcommit_memory = %s", strings.TrimSpace(string(mode)))
	}
	exe := link(t, t.TempDir(), withRuntime([]byte(mappingHarness)))
	cmd := exec.Command(exe)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("harness: %v", err)
		}
	}()
	var words [2]uint64
	if err := binary.Read(stdout, binary.LittleEndian, &words); err != nil {
		t.Fatalf("reading top and L: %v", err)
	}
	top, l := words[0], words[1]
	if top-l != stackDefault {
		t.Errorf("top - L = %d, want S = %d", top-l, stackDefault)
	}
	ms := readSmaps(t, cmd.Process.Pid)
	stack := containing(ms, l-4096, top)
	switch {
	case stack == nil:
		t.Errorf("no mapping contains the stack and reserve [%#x, %#x)", l-4096, top)
	case stack.perms != "rw-p":
		t.Errorf("stack mapping %#x-%#x is %s, want rw-p", stack.start, stack.end, stack.perms)
	case len(stack.flags) == 0:
		t.Error("stack mapping has no VmFlags")
	case strings.Contains(" "+strings.Join(stack.flags, " ")+" ", " nr "):
		t.Errorf("stack mapping has nr (MAP_NORESERVE) in VmFlags %v", stack.flags)
	}
	guard := containing(ms, l-8192, l-4096)
	if guard == nil || guard.perms != "---p" {
		t.Errorf("guard page [%#x, %#x): mapping %+v, want ---p", l-8192, l-4096, guard)
	}
	if _, err := io.WriteString(stdin, "x"); err != nil {
		t.Fatal(err)
	}
}

// TestOverflowHandlerReserve enters aether.stackoverflow with %rsp = L - 8,
// the lowest entry D-011 allows. It must write exactly the fatal error and
// exit 2 without faulting, also with stderr closed, using at most 24 bytes
// of the reserve.
func TestOverflowHandlerReserve(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh to close stderr with")
	}
	harness := fmt.Sprintf(`	.text
	.globl	main.main
main.main:
	movq	%s(%%rip), %%rsp
	subq	$8, %%rsp
	jmp	%s
`, symStackLimit, symStackOverflow) + mainStack(16)
	exe := link(t, t.TempDir(), withRuntime([]byte(harness)))
	if got, code := execute(t, exe, nil); got != "fatal error: stack overflow\n" || code != 2 {
		t.Errorf("stderr %q, exit %d; want the fatal error, exit 2", got, code)
	}
	cmd := exec.Command("/bin/sh", "-c", `exec "$0" 2>&-`, exe)
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Errorf("with stderr closed: %v, want exit status 2", err)
	}
}

// TestFrameLargerThanStack calls a function whose frame is larger than S.
// The prologue check must reject it before anything is written, rather
// than let its frame skip the guard page. A frame that large would need
// millions of IR values, so the test builds a small function and enlarges
// its frame in the assembly: only the prologue's two constants change.
func TestFrameLargerThanStack(t *testing.T) {
	p := &ir.Program{}
	m := p.NewFunc("main", "p.go", p1)
	big := p.NewFunc("big", "p.go", p1)
	big.Println(big.Entry(), []*ir.Instr{big.Const(big.Entry(), 1, p1)}, p1)
	big.Ret(big.Entry(), p1)
	mb := m.Entry()
	m.Println(mb, []*ir.Instr{m.Const(mb, 3, p1)}, p1)
	m.Call(mb, big, nil, p1)
	m.Ret(mb, p1)
	asm, err := Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	const size = 2 * stackDefault
	head, body, ok := bytes.Cut(asm, []byte("main.big:\n"))
	old := "\taddq\t$24, %rax\n\tcmpq\t%rax, %rsp\n\tjb\taether.stackoverflow\n\tpushq\t%rbp\n\tmovq\t%rsp, %rbp\n\tsubq\t$16, %rsp\n"
	if !ok || !bytes.Contains(body, []byte(old)) {
		t.Fatalf("unexpected prologue for big:\n%s", asm)
	}
	body = bytes.Replace(body, []byte(old), fmt.Appendf(nil, "\taddq\t$%d, %%rax\n\tcmpq\t%%rax, %%rsp\n\tjb\taether.stackoverflow\n\tpushq\t%%rbp\n\tmovq\t%%rsp, %%rbp\n\tsubq\t$%d, %%rsp\n", size+8, size), 1)
	asm = append(append(head, "main.big:\n"...), body...)
	got, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil)
	if want := "3\nfatal error: stack overflow\n"; got != want || code != 2 {
		t.Errorf("stderr %q, exit %d; want %q, exit 2", got, code, want)
	}
}
