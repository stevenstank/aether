// Package syscall is Aether's replacement of gc's syscall (D-054): the
// Linux x86-64 system calls package time and Aether's os use, over the
// system-call primitive of D-051, with gc's types, errors, and names.
//
// gc's package is much larger, and stands on its runtime; what Aether's
// lacks, a program using it is unsupported for.
package syscall

import (
	errorspkg "errors"
	"internal/oserror"
	"unsafe"
)

// rawSyscall6 makes the Linux x86-64 system call trap with six arguments
// and returns its result, the negated errno if in [-4095, -1] (D-051): an
// intrinsic.
func rawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) int

// RawSyscall6 makes the system call trap, as gc's does on Linux x86-64.
func RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno) {
	r := rawSyscall6(trap, a1, a2, a3, a4, a5, a6)
	if r < 0 && r > -4096 {
		return ^uintptr(0), 0, Errno(-r)
	}
	return uintptr(r), 0, 0
}

// RawSyscall, Syscall, and Syscall6 are RawSyscall6 with fewer arguments,
// or, the goroutines of an Aether program never running in parallel
// (D-049), the same.
func RawSyscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	return RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
}

func Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	return RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
}

func Syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno) {
	return RawSyscall6(trap, a1, a2, a3, a4, a5, a6)
}

// The system calls this package makes, by number.
const (
	SYS_READ   = 0
	SYS_WRITE  = 1
	SYS_CLOSE  = 3
	SYS_LSEEK  = 8
	SYS_GETPID = 39
	SYS_KILL   = 62
	SYS_OPENAT = 257
)

// The flags of Open, as gc's.
const (
	O_RDONLY    = 0x0
	O_WRONLY    = 0x1
	O_RDWR      = 0x2
	O_CREAT     = 0x40
	O_EXCL      = 0x80
	O_NOCTTY    = 0x100
	O_TRUNC     = 0x200
	O_APPEND    = 0x400
	O_NONBLOCK  = 0x800
	O_SYNC      = 0x101000
	O_DIRECTORY = 0x10000
	O_NOFOLLOW  = 0x20000
	O_CLOEXEC   = 0x80000
	O_LARGEFILE = 0x0

	_AT_FDCWD = -0x64
)

// atFDCWD is _AT_FDCWD as a uintptr.
const atFDCWD = ^uintptr(-_AT_FDCWD - 1)

// call makes the system call trap again after EINTR and returns its
// result or its error number, nil for none.
func call(trap, a1, a2, a3, a4 uintptr) (int, error) {
	for {
		r := rawSyscall6(trap, a1, a2, a3, a4, 0, 0)
		if r == -int(EINTR) {
			continue
		}
		if r < 0 && r > -4096 {
			return 0, errnoErr(Errno(-r))
		}
		return r, nil
	}
}

// errnoErr returns e as an error, gc's preallocated values for the common
// ones.
func errnoErr(e Errno) error {
	switch e {
	case EAGAIN:
		return errEAGAIN
	case EINVAL:
		return errEINVAL
	case ENOENT:
		return errENOENT
	}
	return e
}

var (
	errEAGAIN error = EAGAIN
	errEINVAL error = EINVAL
	errENOENT error = ENOENT
)

// BytePtrFromString returns a pointer to s and a 0 byte, or EINVAL if s
// has a 0 byte.
func BytePtrFromString(s string) (*byte, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return nil, EINVAL
		}
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0], nil
}

// Open opens the file path, as gc's: openat at the working directory,
// with O_LARGEFILE and O_CLOEXEC as the caller's mode has them.
func Open(path string, mode int, perm uint32) (fd int, err error) {
	p, err := BytePtrFromString(path)
	if err != nil {
		return -1, err
	}
	fd, err = call(SYS_OPENAT, atFDCWD, uintptr(unsafe.Pointer(p)), uintptr(mode|O_LARGEFILE), uintptr(perm))
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func Read(fd int, p []byte) (n int, err error) {
	n, err = call(SYS_READ, uintptr(fd), uintptr(unsafe.Pointer(unsafe.SliceData(p))), uintptr(len(p)), 0)
	if err != nil {
		return -1, err
	}
	return n, nil
}

func Write(fd int, p []byte) (n int, err error) {
	n, err = call(SYS_WRITE, uintptr(fd), uintptr(unsafe.Pointer(unsafe.SliceData(p))), uintptr(len(p)), 0)
	if err != nil {
		return -1, err
	}
	return n, nil
}

func Close(fd int) error {
	_, err := call(SYS_CLOSE, uintptr(fd), 0, 0, 0)
	return err
}

func Seek(fd int, offset int64, whence int) (off int64, err error) {
	r, err := call(SYS_LSEEK, uintptr(fd), uintptr(offset), uintptr(whence), 0)
	if err != nil {
		return -1, err
	}
	return int64(r), nil
}

func Getpid() (pid int) {
	r, _ := call(SYS_GETPID, 0, 0, 0, 0)
	return r
}

func Kill(pid int, sig Signal) error {
	_, err := call(SYS_KILL, uintptr(pid), uintptr(sig), 0, 0)
	return err
}

// An Errno is an unsigned number describing an error condition.
type Errno uintptr

func (e Errno) Error() string {
	if 0 <= int(e) && int(e) < len(errors) {
		s := errors[e]
		if s != "" {
			return s
		}
	}
	return "errno " + itoa(int(e))
}

func (e Errno) Is(target error) bool {
	switch target {
	case oserror.ErrPermission:
		return e == EACCES || e == EPERM
	case oserror.ErrExist:
		return e == EEXIST || e == ENOTEMPTY
	case oserror.ErrNotExist:
		return e == ENOENT
	case errorspkg.ErrUnsupported:
		return e == ENOSYS || e == ENOTSUP || e == EOPNOTSUPP
	}
	return false
}

func (e Errno) Temporary() bool {
	return e == EINTR || e == EMFILE || e == ENFILE || e.Timeout()
}

func (e Errno) Timeout() bool {
	return e == EAGAIN || e == EWOULDBLOCK || e == ETIMEDOUT
}

// A Signal is a number describing a process signal.
type Signal int

func (s Signal) Signal() {}

func (s Signal) String() string {
	if 0 <= s && int(s) < len(signals) {
		str := signals[s]
		if str != "" {
			return str
		}
	}
	return "signal " + itoa(int(s))
}

func itoa(n int) string {
	u := uint(n)
	if n < 0 {
		u = -u
	}
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
		if u == 0 {
			break
		}
	}
	if n < 0 {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
