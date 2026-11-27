// Package os is Aether's replacement of gc's os package (D-049, increment
// 11.6, and D-051): the program's arguments, environment, and exit, and
// its files: the standard input, output, and error, and files it opens,
// creates, and removes by name. Its functions behave as gc's, with gc's
// errors; their system calls are one primitive, syscall6, and their
// numbers and structures are Linux x86-64's. The functions without a body
// are intrinsics.
package os

import (
	"io"
	"syscall"
	"unsafe"
)

// Args are the program's arguments, its name first.
var Args []string

func init() {
	Args = make([]string, argc())
	for i := range Args {
		Args[i] = arg(i)
	}
}

func argc() int
func arg(i int) string

// Exit ends the program with the exit status code. Deferred calls do not
// run.
func Exit(code int)

// A File is an open file descriptor.
type File struct {
	fd         int
	name       string
	closed     bool
	appendMode bool // opened with O_APPEND
	kind       int8 // fileUnknown, fileBlocking, or filePollable
	dir        any  // a directory's *dirState (stat.go), untyped so that a File's type does not keep directory code
}

// Stdin, Stdout, and Stderr are the files of the standard input, standard
// output, and standard error.
var (
	Stdin  = &File{fd: 0, name: "/dev/stdin"}
	Stdout = &File{fd: 1, name: "/dev/stdout"}
	Stderr = &File{fd: 2, name: "/dev/stderr"}
)

// Name returns the name of the file.
func (f *File) Name() string { return f.name }

// Fd returns the file's descriptor, or ^uintptr(0) once it is closed.
func (f *File) Fd() uintptr {
	if f == nil || f.closed {
		return ^uintptr(0)
	}
	return uintptr(f.fd)
}

// wrapErr is gc's: err with the operation and the file's name, but io.EOF
// as it is.
func (f *File) wrapErr(op string, err error) error {
	if err == nil || err == io.EOF {
		return err
	}
	return &PathError{Op: op, Path: f.name, Err: err}
}

// Read reads up to len(b) bytes from the file into b, with one read
// system call. At end of file, it returns 0 and io.EOF.
func (f *File) Read(b []byte) (n int, err error) {
	if f == nil {
		return 0, ErrInvalid
	}
	if f.closed {
		return 0, f.wrapErr("read", ErrClosed)
	}
	// As gc's internal/poll.(*FD).Read: no system call for an empty b, at
	// most maxRW bytes, retried after EINTR, and 0 bytes read without an
	// error the end of the file.
	if len(b) == 0 {
		return 0, nil
	}
	if len(b) > maxRW {
		b = b[:maxRW]
	}
	// A pipe, a terminal, or a socket may have nothing to read: the other
	// goroutines run until it has (D-058).
	if f.pollable() {
		for !ioReady(f.fd, false) {
			ioWait(f.fd, false)
			if f.closed {
				return 0, f.wrapErr("read", ErrClosed)
			}
		}
	}
	for {
		m := read(f.fd, b)
		if m == -eintr {
			continue
		}
		if m < 0 {
			return 0, f.wrapErr("read", syscall.Errno(-m))
		}
		if m == 0 {
			return 0, io.EOF
		}
		return m, nil
	}
}

// maxRW is the most bytes gc reads at once (internal/poll).
const maxRW = 1 << 30

// The kinds of a File's descriptor, which pollable finds.
const (
	fileUnknown  = 0
	fileBlocking = 1 // a regular file, a directory, or a block device, always ready
	filePollable = 2 // a pipe, a character device, or a socket
)

// pollable reports whether a read of the file can wait for data, which
// poll(2) can tell: its descriptor is a pipe, a character device such as
// a terminal, or a socket.
func (f *File) pollable() bool {
	if f.kind == fileUnknown {
		f.kind = fileBlocking
		var w statWords
		if _, err := call(sysFstat, uintptr(f.fd), uintptr(unsafe.Pointer(&w)), 0, 0); err == nil {
			switch w.stat().mode & 0xf000 {
			case 0x1000, 0x2000, 0xc000: // S_IFIFO, S_IFCHR, S_IFSOCK
				f.kind = filePollable
			}
		}
	}
	return f.kind == filePollable
}

// ioReady, ioWait, and ioClose are runtime's (frontend.Linknames, D-058):
// whether fd is ready to read without waiting; a wait, running the other
// goroutines, until it may be or it is closed; and the end of the waits
// on fd, which is being closed.
func ioReady(fd int, write bool) bool
func ioWait(fd int, write bool)
func ioClose(fd int)

// read makes one read system call and returns its result: the bytes
// read, or the negated errno.
func read(fd int, b []byte) int

// Write writes b to the file, all of it unless an error occurs.
func (f *File) Write(b []byte) (n int, err error) {
	if f == nil {
		return 0, ErrInvalid
	}
	if f.closed {
		return 0, f.wrapErr("write", ErrClosed)
	}
	// As gc's internal/poll.(*FD).Write: one write at least, retried after
	// EINTR, continued after a short one.
	for {
		m := write(f.fd, b[n:])
		if m == -eintr {
			continue
		}
		if m > 0 {
			n += m
		}
		if m < 0 {
			return n, f.wrapErr("write", syscall.Errno(-m))
		}
		if n == len(b) {
			return n, nil
		}
		if m == 0 {
			return n, f.wrapErr("write", io.ErrUnexpectedEOF)
		}
	}
}

// WriteString is Write of the bytes of s.
func (f *File) WriteString(s string) (n int, err error) {
	return f.Write(unsafe.Slice(unsafe.StringData(s), len(s)))
}

// write makes one write system call and returns its result: the bytes
// written, or the negated errno.
func write(fd int, b []byte) int

// Close closes the file. Closing it again is an error.
func (f *File) Close() error {
	if f == nil {
		return ErrInvalid
	}
	if f.closed {
		return f.wrapErr("close", ErrClosed)
	}
	f.closed = true
	if f.kind == filePollable {
		ioClose(f.fd)
	}
	// As gc's: no retry after EINTR, which leaves the descriptor closed.
	if r := syscall6(sysClose, uintptr(f.fd), 0, 0, 0, 0, 0); r < 0 {
		return f.wrapErr("close", syscall.Errno(-r))
	}
	return nil
}
