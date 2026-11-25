package os

import (
	"errors"
	"io"
	"slices"
	"unsafe"
)

// Flags to OpenFile, Linux x86-64's.
const (
	O_RDONLY int = 0x0
	O_WRONLY int = 0x1
	O_RDWR   int = 0x2
	O_APPEND int = 0x400
	O_CREATE int = 0x40
	O_EXCL   int = 0x80
	O_SYNC   int = 0x101000
	O_TRUNC  int = 0x200
)

// Seek whence values.
//
// Deprecated: Use io.SeekStart, io.SeekCurrent, and io.SeekEnd.
const (
	SEEK_SET int = 0
	SEEK_CUR int = 1
	SEEK_END int = 2
)

const (
	PathSeparator     = '/' // OS-specific path separator
	PathListSeparator = ':' // OS-specific path list separator
)

// DevNull is the name of the operating system's “null device.”
const DevNull = "/dev/null"

// IsPathSeparator reports whether c is a directory separator character.
func IsPathSeparator(c uint8) bool { return PathSeparator == c }

// Open opens the named file for reading.
func Open(name string) (*File, error) { return OpenFile(name, O_RDONLY, 0) }

// Create creates or truncates the named file, with mode 0o666 (before
// umask) if it is created.
func Create(name string) (*File, error) {
	return OpenFile(name, O_RDWR|O_CREATE|O_TRUNC, 0666)
}

// OpenFile opens the named file with the flags (O_RDONLY etc.), creating
// it with mode perm (before umask) if O_CREATE is given and it does not
// exist.
func OpenFile(name string, flag int, perm FileMode) (*File, error) {
	fd, err := pathCall(sysOpenat, name, uintptr(flag|oCloexec), uintptr(syscallMode(perm)))
	if err != nil {
		return nil, &PathError{Op: "open", Path: name, Err: err}
	}
	return &File{fd: fd, name: name, appendMode: flag&O_APPEND != 0}, nil
}

// syscallMode is gc's: the mode bits of the open and mkdir system calls.
func syscallMode(i FileMode) (o uint32) {
	o |= uint32(i.Perm())
	if i&ModeSetuid != 0 {
		o |= 0x800 // S_ISUID
	}
	if i&ModeSetgid != 0 {
		o |= 0x400 // S_ISGID
	}
	if i&ModeSticky != 0 {
		o |= 0x200 // S_ISVTX
	}
	return
}

// ReadFile reads the named file and returns its contents. Reaching the end
// of the file is not an error.
func ReadFile(name string) ([]byte, error) {
	f, err := Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var size int64
	if s, err := fstat(f); err == nil {
		size = s.size
	}
	return readFileContents(size, f.Read)
}

// readFileContents is gc's, from $GOROOT/src/os/file.go.
func readFileContents(statSize int64, read func([]byte) (int, error)) ([]byte, error) {
	zeroSize := statSize == 0

	// Figure out how big to make the initial slice. For files with known size
	// that fit in memory, use that size + 1. Otherwise, use a small buffer and
	// we'll grow.
	var size int
	if int64(int(statSize)) == statSize {
		size = int(statSize)
	}
	size++ // one byte for final read at EOF

	const minBuf = 512
	// If a file claims a small size, read at least 512 bytes. In particular,
	// files in Linux's /proc claim size 0 but then do not work right if read in
	// small pieces, so an initial read of 1 byte would not work correctly.
	if size < minBuf {
		size = minBuf
	}

	data := make([]byte, 0, size)
	for {
		n, err := read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}

		// If we're either out of capacity or if the file was a /proc-like zero
		// sized file, grow the buffer. Per Issue 72080, we always want to issue
		// Read calls on zero-length files with a non-tiny buffer size.
		capRemain := cap(data) - len(data)
		if capRemain == 0 || (zeroSize && capRemain < minBuf) {
			data = slices.Grow(data, minBuf)
		}
	}
}

// WriteFile writes data to the named file, creating it with mode perm
// (before umask) if necessary, and truncating it first otherwise.
func WriteFile(name string, data []byte, perm FileMode) error {
	f, err := OpenFile(name, O_WRONLY|O_CREATE|O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err1 := f.Close(); err1 != nil && err == nil {
		err = err1
	}
	return err
}

// fstat is the fstat system call of the open file f.
func fstat(f *File) (fileStat, error) {
	var w statWords
	if f.closed {
		return fileStat{}, ErrClosed
	}
	if _, err := call(sysFstat, uintptr(f.fd), uintptr(unsafe.Pointer(&w)), 0, 0); err != nil {
		return fileStat{}, err
	}
	return w.stat(), nil
}

// fileCall is call on the open file f, its descriptor first: ErrClosed if
// f is closed.
func (f *File) fileCall(trap, a2, a3, a4 uintptr) (int, error) {
	if f.closed {
		return 0, ErrClosed
	}
	return call(trap, uintptr(f.fd), a2, a3, a4)
}

// ReadAt reads len(b) bytes from the file at offset off. It returns an
// error, io.EOF at the end of the file, if it reads fewer.
func (f *File) ReadAt(b []byte, off int64) (n int, err error) {
	if f == nil {
		return 0, ErrInvalid
	}
	if off < 0 {
		return 0, &PathError{Op: "readat", Path: f.name, Err: errors.New("negative offset")}
	}
	for len(b) > 0 {
		m, e := f.pread(b, off)
		if e != nil {
			err = f.wrapErr("read", e)
			break
		}
		n += m
		b = b[m:]
		off += int64(m)
	}
	return
}

// pread is gc's internal/poll.(*FD).Pread: one system call of at most
// maxRW bytes, and io.EOF for 0 bytes read.
func (f *File) pread(b []byte, off int64) (int, error) {
	if len(b) > maxRW {
		b = b[:maxRW]
	}
	n, err := f.fileCall(sysPread64, addr(b), uintptr(len(b)), uintptr(off))
	if err == nil && n == 0 && len(b) > 0 {
		err = io.EOF
	}
	return n, err
}

var errWriteAtInAppendMode = errors.New("os: invalid use of WriteAt on file opened with O_APPEND")

// WriteAt writes len(b) bytes to the file at offset off. It returns an
// error if it writes fewer, or if the file was opened with O_APPEND.
func (f *File) WriteAt(b []byte, off int64) (n int, err error) {
	if f == nil {
		return 0, ErrInvalid
	}
	if f.appendMode {
		return 0, errWriteAtInAppendMode
	}
	if off < 0 {
		return 0, &PathError{Op: "writeat", Path: f.name, Err: errors.New("negative offset")}
	}
	for len(b) > 0 {
		m, e := f.pwrite(b, off)
		if e != nil {
			err = f.wrapErr("write", e)
			break
		}
		n += m
		b = b[m:]
		off += int64(m)
	}
	return
}

// pwrite is gc's internal/poll.(*FD).Pwrite: system calls until all of b
// is written, io.ErrUnexpectedEOF if one writes nothing.
func (f *File) pwrite(b []byte, off int64) (int, error) {
	var nn int
	for nn < len(b) {
		n, err := f.fileCall(sysPwrite64, addr(b[nn:]), uintptr(len(b)-nn), uintptr(off+int64(nn)))
		if err != nil {
			return nn, err
		}
		if n == 0 {
			return nn, io.ErrUnexpectedEOF
		}
		nn += n
	}
	return nn, nil
}

// Seek sets the offset of the file's next Read or Write: offset from the
// start of the file (whence 0), from the current offset (1), or from its
// end (2).
func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	if f == nil {
		return 0, ErrInvalid
	}
	r, e := f.fileCall(sysLseek, uintptr(offset), uintptr(whence), 0)
	if e != nil {
		return 0, f.wrapErr("seek", e)
	}
	return int64(r), nil
}

// Sync commits the file's contents to stable storage.
func (f *File) Sync() error {
	if f == nil {
		return ErrInvalid
	}
	if _, e := f.fileCall(sysFsync, 0, 0, 0); e != nil {
		return f.wrapErr("sync", e)
	}
	return nil
}

// Truncate changes the size of the file.
func (f *File) Truncate(size int64) error {
	if f == nil {
		return ErrInvalid
	}
	if _, e := f.fileCall(sysFtruncate, uintptr(size), 0, 0); e != nil {
		return f.wrapErr("truncate", e)
	}
	return nil
}

// Mkdir creates the named directory with mode perm (before umask).
func Mkdir(name string, perm FileMode) error {
	if _, e := pathCall(sysMkdirat, name, uintptr(syscallMode(perm)), 0); e != nil {
		return &PathError{Op: "mkdir", Path: name, Err: e}
	}
	return nil
}

// MkdirAll is gc's: it creates the directory path and any parents it
// lacks, with mode perm (before umask). It does nothing if path is a
// directory.
func MkdirAll(path string, perm FileMode) error {
	// Fast path: if we can tell whether path is a directory or file, stop with success or error.
	dir, err := statName("stat", path, 0)
	if err == nil {
		if dir.isDir() {
			return nil
		}
		return &PathError{Op: "mkdir", Path: path, Err: errno(enotdir)}
	}

	// Slow path: make sure parent exists and then call Mkdir for path.
	i := len(path) - 1
	for i >= 0 && IsPathSeparator(path[i]) {
		i--
	}
	for i >= 0 && !IsPathSeparator(path[i]) {
		i--
	}
	if i < 0 {
		i = 0
	}
	if parent := path[:i]; len(parent) > 0 {
		err = MkdirAll(parent, perm)
		if err != nil {
			return err
		}
	}

	// Parent now exists; invoke Mkdir and use its result.
	err = Mkdir(path, perm)
	if err != nil {
		// Handle arguments like "foo/." by
		// double-checking that directory doesn't exist.
		dir, err1 := statName("lstat", path, atSymlinkNofollow)
		if err1 == nil && dir.isDir() {
			return nil
		}
		return err
	}
	return nil
}

// Remove removes the named file or empty directory.
func Remove(name string) error {
	_, e := pathCall(sysUnlinkat, name, 0, 0)
	if e == nil {
		return nil
	}
	_, e1 := pathCall(sysUnlinkat, name, atRemoveDir, 0)
	if e1 == nil {
		return nil
	}
	// Both failed: as gc's, rmdir's error, unless it is ENOTDIR.
	if e1 != errno(enotdir) {
		e = e1
	}
	return &PathError{Op: "remove", Path: name, Err: e}
}

// Rename is gc's: it renames (moves) oldpath to newpath, replacing
// newpath if it exists and is not a directory.
func Rename(oldpath, newpath string) error {
	fi, err := statName("lstat", newpath, atSymlinkNofollow)
	if err == nil && fi.isDir() {
		if ofi, err := statName("lstat", oldpath, atSymlinkNofollow); err != nil {
			return &LinkError{"rename", oldpath, newpath, err.(*PathError).Err}
		} else if newpath == oldpath || !sameFile(fi, ofi) {
			return &LinkError{"rename", oldpath, newpath, errno(eexist)}
		}
	}
	o, err := cstring(oldpath)
	if err == nil {
		var n []byte
		n, err = cstring(newpath)
		if err == nil {
			_, err = call(sysRenameat, atFDCWD, addr(o), atFDCWD, addr(n))
		}
	}
	if err != nil {
		return &LinkError{"rename", oldpath, newpath, err}
	}
	return nil
}

// Getwd returns the path of the working directory: $PWD if it names it,
// as gc's, and otherwise the getcwd system call's.
func Getwd() (dir string, err error) {
	dir = Getenv("PWD")
	if len(dir) > 0 && dir[0] == '/' {
		dot, err := statName("stat", ".", 0)
		if err != nil {
			return "", err
		}
		d, err := statName("stat", dir, 0)
		if err == nil && sameFile(dot, d) {
			return dir, nil
		}
	}
	// gc's syscall.Getwd. Where gc then searches the parent directories,
	// after ENAMETOOLONG, Aether returns the error (SEMANTICS.md).
	var buf [4096]byte
	n, err := call(sysGetcwd, addr(buf[:]), uintptr(len(buf)), 0, 0)
	switch {
	case err != nil:
	case n < 1 || n > len(buf) || buf[n-1] != 0:
		err = errno(einval)
	case buf[0] != '/':
		err = errno(enoent)
	default:
		return string(buf[:n-1]), nil
	}
	return "", NewSyscallError("getwd", err)
}

// Chdir changes the working directory to dir.
func Chdir(dir string) error {
	p, e := cstring(dir)
	if e == nil {
		if r := syscall6(sysChdir, addr(p), 0, 0, 0, 0, 0); r < 0 {
			e = errno(-r)
		}
	}
	if e != nil {
		return &PathError{Op: "chdir", Path: dir, Err: e}
	}
	return nil
}

// TempDir returns $TMPDIR, or /tmp if it is empty.
func TempDir() string {
	dir := Getenv("TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return dir
}

// UserHomeDir returns $HOME, or an error if it is empty.
func UserHomeDir() (string, error) {
	if v := Getenv("HOME"); v != "" {
		return v, nil
	}
	return "", errors.New("$HOME is not defined")
}

// A FileMode is a file's mode and permission bits. It is gc's io/fs
// FileMode, which Aether cannot import yet (SEMANTICS.md).
type FileMode uint32

// The defined file mode bits are the most significant bits of the
// FileMode. The nine least-significant bits are the standard Unix rwxrwxrwx
// permissions.
const (
	// The single letters are the abbreviations
	// used by the String method's formatting.
	ModeDir        FileMode = 1 << (32 - 1 - iota) // d: is a directory
	ModeAppend                                     // a: append-only
	ModeExclusive                                  // l: exclusive use
	ModeTemporary                                  // T: temporary file; Plan 9 only
	ModeSymlink                                    // L: symbolic link
	ModeDevice                                     // D: device file
	ModeNamedPipe                                  // p: named pipe (FIFO)
	ModeSocket                                     // S: Unix domain socket
	ModeSetuid                                     // u: setuid
	ModeSetgid                                     // g: setgid
	ModeCharDevice                                 // c: Unix character device, when ModeDevice is set
	ModeSticky                                     // t: sticky
	ModeIrregular                                  // ?: non-regular file; nothing else is known about this file

	// Mask for the type bits. For regular files, none will be set.
	ModeType = ModeDir | ModeSymlink | ModeNamedPipe | ModeSocket | ModeDevice | ModeCharDevice | ModeIrregular

	ModePerm FileMode = 0777 // Unix permission bits
)

func (m FileMode) String() string {
	const str = "dalTLDpSugct?"
	var buf [32]byte // Mode is uint32.
	w := 0
	for i, c := range str {
		if m&(1<<uint(32-1-i)) != 0 {
			buf[w] = byte(c)
			w++
		}
	}
	if w == 0 {
		buf[w] = '-'
		w++
	}
	const rwx = "rwxrwxrwx"
	for i, c := range rwx {
		if m&(1<<uint(9-1-i)) != 0 {
			buf[w] = byte(c)
		} else {
			buf[w] = '-'
		}
		w++
	}
	return string(buf[:w])
}

// IsDir reports whether m describes a directory.
func (m FileMode) IsDir() bool { return m&ModeDir != 0 }

// IsRegular reports whether m describes a regular file.
func (m FileMode) IsRegular() bool { return m&ModeType == 0 }

// Perm returns the Unix permission bits in m (m & ModePerm).
func (m FileMode) Perm() FileMode { return m & ModePerm }

// Type returns type bits in m (m & ModeType).
func (m FileMode) Type() FileMode { return m & ModeType }
