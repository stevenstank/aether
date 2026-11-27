package interp

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// The system calls of SysCall the interpreter makes (D-051): exactly those
// Aether's os package makes, by their Linux x86-64 numbers, which are the
// host's.
const (
	sysRead       = 0
	sysWrite      = 1
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
	// time's (D-054).
	sysClockGettime   = 228
	sysClockNanosleep = 230
	// runtime's, for os (D-058).
	sysPoll = 7
	einval  = 22 // EINVAL
)

// atFDCWD is AT_FDCWD, the working directory as a directory descriptor.
const atFDCWD = -100

// oPath is O_PATH, which package syscall does not name.
const oPath = 0x200000

// statSize is the size of the kernel's struct stat.
const statSize = 144

// syscall is SysCall: the system call a[0] with the arguments a[1:]. The
// program's descriptors are translated to the host's, its working
// directory is the process's own (cwd), and its addresses to the bytes of
// the objects they point into. The result is the host's, or the negated
// errno. A system call Aether's os package does not make, or one whose
// memory is not the program's, is an error of the interpreter.
func (p *process) syscall(mem *memory, a [7]int64) (int64, error) {
	switch a[0] {
	case sysClockGettime:
		// A struct timespec of two words.
		b, err := mem.writable(a[2], 16)
		if err != nil {
			return 0, err
		}
		return host(sysClockGettime, uintptr(a[1]), uintptr(unsafe.Pointer(&b[0])), 0, 0), nil
	case sysClockNanosleep:
		if a[4] != 0 {
			return 0, fmt.Errorf("clock_nanosleep with a remainder is not emulated")
		}
		b, err := mem.bytes(a[3], 16)
		if err != nil {
			return 0, err
		}
		req := append([]byte(nil), b...)
		return host(sysClockNanosleep, uintptr(a[1]), uintptr(a[2]), uintptr(unsafe.Pointer(&req[0])), 0), nil
	case sysRead:
		b, err := mem.writable(a[2], a[3])
		if err != nil {
			return 0, err
		}
		return p.read(a[1], b), nil
	case sysWrite:
		b, err := mem.bytes(a[2], a[3])
		if err != nil {
			return 0, err
		}
		return p.write(a[1], b), nil
	case sysClose:
		v, ok := p.fds[a[1]]
		if !ok {
			return -int64(syscall.EBADF), nil
		}
		delete(p.fds, a[1])
		if v.std >= 0 {
			return 0, nil
		}
		return host(sysClose, uintptr(v.host), 0, 0, 0), nil
	case sysPoll:
		// The run's standard input is the bytes given, and the program's
		// other descriptors are files: every one is ready.
		if a[2] < 0 || a[2] > 1<<16 {
			return -einval, nil
		}
		b, err := mem.writable(a[1], 8*a[2])
		if err != nil {
			return 0, err
		}
		ready := int64(0)
		for i := range a[2] {
			e := b[8*i:]
			ev := uint16(e[4]) | uint16(e[5])<<8
			fd := int64(int32(uint32(e[0]) | uint32(e[1])<<8 | uint32(e[2])<<16 | uint32(e[3])<<24))
			if _, ok := p.fds[fd]; !ok {
				ev = 0x20 // POLLNVAL
			}
			if ev &= 0x20 | 0x1 | 0x4; ev != 0 { // POLLIN, POLLOUT
				ready++
			}
			e[6], e[7] = byte(ev), byte(ev>>8)
		}
		return ready, nil
	case sysFstat:
		if v, ok := p.fds[a[1]]; ok && v.std >= 0 {
			// A standard file is a pipe: its struct stat is zero but for
			// st_mode, S_IFIFO and 0600.
			b, err := mem.writable(a[2], statSize)
			if err != nil {
				return 0, err
			}
			clear(b)
			b[24], b[25] = 0x80, 0x11
			return 0, nil
		}
		fd, err := p.hostFD(a[1])
		if err != nil || fd < 0 {
			return int64(fd), err
		}
		b, err := mem.writable(a[2], statSize)
		if err != nil {
			return 0, err
		}
		return host(sysFstat, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), 0, 0), nil
	case sysLseek, sysFsync, sysFtruncate:
		fd, err := p.hostFD(a[1])
		if err != nil || fd < 0 {
			return int64(fd), err
		}
		return host(uintptr(a[0]), uintptr(fd), uintptr(a[2]), uintptr(a[3]), 0), nil
	case sysPread64, sysPwrite64:
		fd, err := p.hostFD(a[1])
		if err != nil || fd < 0 {
			return int64(fd), err
		}
		var b []byte
		if a[0] == sysPread64 {
			b, err = mem.writable(a[2], a[3])
		} else {
			b, err = mem.bytes(a[2], a[3])
		}
		if err != nil || len(b) == 0 {
			return 0, err
		}
		return host(uintptr(a[0]), uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(a[4])), nil
	case sysGetcwd:
		return p.getcwd(mem, a[1], a[2])
	case sysChdir:
		return p.chdir(mem, a[1])
	case sysOpenat, sysMkdirat, sysUnlinkat:
		dir, err := p.dirFD(a[1])
		if err != nil {
			return 0, err
		}
		path, err := mem.cstring(a[2])
		if err != nil {
			return 0, err
		}
		flags := uintptr(a[3])
		if a[0] == sysOpenat {
			flags |= syscall.O_CLOEXEC // the host's descriptors stay its own
		}
		r := host(uintptr(a[0]), uintptr(dir), uintptr(unsafe.Pointer(&path[0])), flags, uintptr(a[4]))
		if a[0] != sysOpenat || r < 0 {
			return r, nil
		}
		return p.newFD(int(r)), nil
	case sysNewfstatat:
		dir, err := p.dirFD(a[1])
		if err != nil {
			return 0, err
		}
		path, err := mem.cstring(a[2])
		if err != nil {
			return 0, err
		}
		b, err := mem.writable(a[3], statSize)
		if err != nil {
			return 0, err
		}
		return host(sysNewfstatat, uintptr(dir), uintptr(unsafe.Pointer(&path[0])), uintptr(unsafe.Pointer(&b[0])), uintptr(a[4])), nil
	case sysRenameat:
		odir, err := p.dirFD(a[1])
		if err != nil {
			return 0, err
		}
		ndir, err := p.dirFD(a[3])
		if err != nil {
			return 0, err
		}
		oldpath, err := mem.cstring(a[2])
		if err != nil {
			return 0, err
		}
		newpath, err := mem.cstring(a[4])
		if err != nil {
			return 0, err
		}
		_, _, e := syscall.Syscall6(sysRenameat, uintptr(odir), uintptr(unsafe.Pointer(&oldpath[0])), uintptr(ndir), uintptr(unsafe.Pointer(&newpath[0])), 0, 0)
		return -int64(e), nil
	}
	return 0, fmt.Errorf("system call %d is not emulated", a[0])
}

// host makes the host's system call trap and returns its result, or the
// negated errno.
func host(trap, a1, a2, a3, a4 uintptr) int64 {
	r, _, e := syscall.Syscall6(trap, a1, a2, a3, a4, 0, 0)
	if e != 0 {
		return -int64(e)
	}
	return int64(r)
}

func hostRead(fd int, b []byte) int64 {
	if len(b) == 0 {
		return host(sysRead, uintptr(fd), 0, 0, 0)
	}
	return host(sysRead, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
}

func hostWrite(fd int, b []byte) int64 {
	if len(b) == 0 {
		return host(sysWrite, uintptr(fd), 0, 0, 0)
	}
	return host(sysWrite, uintptr(fd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
}

// hostFD returns the host's descriptor of the program's fd, or -EBADF if
// fd is not open. Only reads and writes of a standard file are emulated.
func (p *process) hostFD(fd int64) (int, error) {
	v, ok := p.fds[fd]
	switch {
	case !ok:
		return -int(syscall.EBADF), nil
	case v.std >= 0:
		return 0, fmt.Errorf("system call on standard file %d is not emulated", fd)
	}
	return v.host, nil
}

// dirFD returns the host's descriptor of the directory descriptor fd of an
// *at system call, which Aether's os package passes only as AT_FDCWD.
func (p *process) dirFD(fd int64) (int, error) {
	if fd != atFDCWD {
		return 0, fmt.Errorf("directory descriptor %d is not emulated", fd)
	}
	return p.workDir()
}

// workDir returns the host's descriptor of the working directory, opened
// on first use.
func (p *process) workDir() (int, error) {
	if p.cwd < 0 {
		dir := p.dir
		if dir == "" {
			dir = "."
		}
		fd, err := syscall.Open(dir, oPath|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
		if err != nil {
			return 0, fmt.Errorf("working directory %s: %v", dir, err)
		}
		p.cwd = fd
	}
	return p.cwd, nil
}

// newFD numbers the host's new descriptor fd as the kernel would: the
// lowest the program has free.
func (p *process) newFD(fd int) int64 {
	var n int64
	for {
		if _, ok := p.fds[n]; !ok {
			p.fds[n] = vfd{std: -1, host: fd}
			return n
		}
		n++
	}
}

// getcwd is the getcwd system call: the working directory's path and a 0
// byte, written to the size bytes at buf; ERANGE if they do not fit.
func (p *process) getcwd(mem *memory, buf, size int64) (int64, error) {
	dir, err := p.workDir()
	if err != nil {
		return 0, err
	}
	path, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(dir))
	if err != nil {
		return 0, err
	}
	n := int64(len(path)) + 1
	if size < n {
		return -int64(syscall.ERANGE), nil
	}
	b, err := mem.writable(buf, n)
	if err != nil {
		return 0, err
	}
	copy(b, path)
	b[n-1] = 0
	return n, nil
}

// chdir is the chdir system call, on the program's working directory: the
// directory must exist and be searchable.
func (p *process) chdir(mem *memory, pathp int64) (int64, error) {
	dir, err := p.workDir()
	if err != nil {
		return 0, err
	}
	path, err := mem.cstring(pathp)
	if err != nil {
		return 0, err
	}
	r := host(sysOpenat, uintptr(dir), uintptr(unsafe.Pointer(&path[0])), oPath|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if r < 0 {
		return r, nil
	}
	if err := syscall.Faccessat(int(r), ".", 1, 0); err != nil { // X_OK
		syscall.Close(int(r))
		return -int64(err.(syscall.Errno)), nil
	}
	syscall.Close(dir)
	p.cwd = int(r)
	return 0, nil
}

// closeFiles closes the host's descriptors of the run.
func (p *process) closeFiles() {
	for _, v := range p.fds {
		if v.std < 0 {
			syscall.Close(v.host)
		}
	}
	if p.cwd >= 0 {
		syscall.Close(p.cwd)
	}
}
