// Package os is Aether's replacement of gc's os package (D-049, increment
// 11.6): the program's arguments, its exit, reads of its standard input
// (11.8), and writes to its standard output and standard error. The
// functions without a body are intrinsics.
package os

import (
	"errors"
	"io"
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

// ErrInvalid is the error of an operation on a nil *File.
var ErrInvalid = errors.New("invalid argument")

// A File is an open file descriptor.
type File struct {
	fd   int
	name string
}

// Stdin, Stdout, and Stderr are the files of the standard input, standard
// output, and standard error.
var (
	Stdin  = &File{0, "/dev/stdin"}
	Stdout = &File{1, "/dev/stdout"}
	Stderr = &File{2, "/dev/stderr"}
)

// Name returns the name of the file.
func (f *File) Name() string { return f.name }

// Read reads up to len(b) bytes from the file into b, with one read
// system call. At end of file, it returns 0 and io.EOF.
func (f *File) Read(b []byte) (n int, err error) {
	if f == nil {
		return 0, ErrInvalid
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
			return 0, &PathError{Op: "read", Path: f.name, Err: errno(-m)}
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
			return n, &PathError{Op: "write", Path: f.name, Err: errno(-m)}
		}
		if n == len(b) {
			return n, nil
		}
		if m == 0 {
			return n, &PathError{Op: "write", Path: f.name, Err: errUnexpectedEOF}
		}
	}
}

// WriteString is Write of the bytes of s.
func (f *File) WriteString(s string) (n int, err error) {
	return f.Write([]byte(s))
}

// write makes one write system call and returns its result: the bytes
// written, or the negated errno.
func write(fd int, b []byte) int

var errUnexpectedEOF = errors.New("unexpected EOF")

// PathError records an error and the operation and file path that caused
// it.
type PathError struct {
	Op   string
	Path string
	Err  error
}

func (e *PathError) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }

func (e *PathError) Unwrap() error { return e.Err }

const eintr = 4

// errno is a Linux error number, with gc's message for each error of the
// read and write system calls (syscall/zerrors_linux_amd64.go).
type errno int

func (e errno) Error() string {
	switch e {
	case 1:
		return "operation not permitted"
	case 5:
		return "input/output error"
	case 9:
		return "bad file descriptor"
	case 11:
		return "resource temporarily unavailable"
	case 14:
		return "bad address"
	case 21:
		return "is a directory"
	case 22:
		return "invalid argument"
	case 27:
		return "file too large"
	case 28:
		return "no space left on device"
	case 32:
		return "broken pipe"
	case 89:
		return "destination address required"
	case 122:
		return "disk quota exceeded"
	}
	return "errno " + itoa(int(e))
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}
