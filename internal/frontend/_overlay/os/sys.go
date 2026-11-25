package os

import (
	"syscall"
	"unsafe"
)

// syscall6 makes the Linux x86-64 system call trap with six arguments and
// returns its result, the negated errno if in [-4095, -1] (D-051). An
// address is passed as its uintptr; its object stays alive, as Aether's
// collector never moves an object and runs only when the program
// allocates.
func syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) int

// The system calls of package os, by number, as gc's syscall makes them
// on Linux x86-64.
const (
	sysClose      = 3
	sysFstat      = 5
	sysLseek      = 8
	sysPread64    = 17
	sysPwrite64   = 18
	sysFsync      = 74
	sysFtruncate  = 77
	sysGetcwd     = 79
	sysChdir      = 80
	sysOpenat     = 257
	sysMkdirat    = 258
	sysNewfstatat = 262
	sysUnlinkat   = 263
	sysRenameat   = 264
)

const (
	atFDCWD           = ^uintptr(99) // AT_FDCWD, -100
	atRemoveDir       = 0x200
	atSymlinkNofollow = 0x100
	oCloexec          = 0x80000
)

// call makes a system call, again after EINTR, as gc's ignoringEINTR, and
// returns its result or its errno.
func call(trap, a1, a2, a3, a4 uintptr) (int, error) {
	for {
		r := syscall6(trap, a1, a2, a3, a4, 0, 0)
		if r == -eintr {
			continue
		}
		if r < 0 && r > -4096 {
			return 0, syscall.Errno(-r)
		}
		return r, nil
	}
}

// cstring returns s and a 0 byte, a path for a system call, or EINVAL if
// s has a 0 byte, as gc's syscall.BytePtrFromString.
func cstring(s string) ([]byte, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return nil, syscall.Errno(einval)
		}
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b, nil
}

// addr is the address of b's first byte, 0 for no bytes.
func addr(b []byte) uintptr { return uintptr(unsafe.Pointer(unsafe.SliceData(b))) }

// pathCall makes the system call trap with AT_FDCWD, the path name, a2,
// and a3, as call.
func pathCall(trap uintptr, name string, a2, a3 uintptr) (int, error) {
	p, err := cstring(name)
	if err != nil {
		return 0, err
	}
	return call(trap, atFDCWD, addr(p), a2, a3)
}

// fileStat is what package os uses of the kernel's struct stat.
type fileStat struct {
	dev, ino uint64
	mode     uint32
	size     int64
}

func (s fileStat) isDir() bool { return s.mode&0xf000 == 0x4000 } // S_IFDIR

func sameFile(a, b fileStat) bool { return a.dev == b.dev && a.ino == b.ino }

// statWords is the kernel's struct stat, 144 bytes.
type statWords [18]uint64

func (w *statWords) stat() fileStat {
	return fileStat{dev: w[0], ino: w[1], mode: uint32(w[3]), size: int64(w[6])}
}

// statName is gc's Stat (op "stat", flags 0) or Lstat (op "lstat",
// atSymlinkNofollow) of name, but only what fileStat records.
func statName(op, name string, flags uintptr) (fileStat, error) {
	var w statWords
	p, err := cstring(name)
	if err == nil {
		_, err = call(sysNewfstatat, atFDCWD, addr(p), uintptr(unsafe.Pointer(&w)), flags)
	}
	if err != nil {
		return fileStat{}, &PathError{Op: op, Path: name, Err: err}
	}
	return w.stat(), nil
}
