// Aether's part of gc's internal/syscall/unix: the function gc's runtime
// implements for it (D-056).

package unix

import "syscall"

// aetherFcntl is gc's runtime fcntl: the result, or -1 and the errno.
func aetherFcntl(fd, cmd, arg int32) (int32, int32) {
	r, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(cmd), uintptr(arg))
	if e != 0 {
		return -1, int32(e)
	}
	return int32(r), 0
}
