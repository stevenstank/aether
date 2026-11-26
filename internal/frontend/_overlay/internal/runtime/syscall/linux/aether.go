// Aether's part of gc's internal/runtime/syscall/linux: Syscall6, which
// gc writes in assembly, over D-051's system-call primitive. The rest of
// the package, and gc's syscall above it, are gc's.

package linux

// syscall6 makes the Linux x86-64 system call trap with six arguments and
// returns its result, the negated errno if in [-4095, -1] (D-051): an
// intrinsic.
func syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) int

// aetherSyscall6 is Syscall6: r2 is always 0, since the primitive returns
// only rax, which no Linux x86-64 system call gc's packages make needs.
func aetherSyscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr) {
	r := syscall6(num, a1, a2, a3, a4, a5, a6)
	if r < 0 && r > -4096 {
		return ^uintptr(0), 0, uintptr(-r)
	}
	return uintptr(r), 0, 0
}
