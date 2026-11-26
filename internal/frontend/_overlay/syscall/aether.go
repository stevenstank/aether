// Aether's part of gc's syscall, which is otherwise gc's own: the
// functions gc's runtime implements (frontend patches give gc's
// declarations bodies calling these), and the environment (env.go).

package syscall

import "unsafe"

// An Aether program's goroutines never run in parallel (D-049), and its
// system calls never block the others' scheduling: there is nothing to
// enter or leave.
func aetherEnterSyscall() {}
func aetherExitSyscall()  {}

func aetherRawSyscallNoError(trap, a1, a2, a3 uintptr) (r1, r2 uintptr) {
	r1, r2, _ = RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
	return
}

func aetherGetpagesize() int { return 4096 }

func aetherExit(code int) {
	RawSyscall6(SYS_EXIT_GROUP, uintptr(code), 0, 0, 0, 0, 0)
}

func aetherGettimeofday(tv *Timeval) Errno {
	_, _, e := RawSyscall6(SYS_GETTIMEOFDAY, uintptr(unsafe.Pointer(tv)), 0, 0, 0, 0, 0)
	return e
}
