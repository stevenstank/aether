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
	// As gc's: no retry after EINTR, which leaves the descriptor closed.
	if r := syscall6(sysClose, uintptr(f.fd), 0, 0, 0, 0, 0); r < 0 {
		return f.wrapErr("close", syscall.Errno(-r))
	}
	return nil
}
