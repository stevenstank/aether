// Package os is Aether's replacement of gc's os package (D-049, increment
// 11.6): the program's arguments, its exit, and writes to its standard
// output and standard error. The functions without a body are intrinsics.
package os

import "errors"

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

// Stdout and Stderr are the files of the standard output and standard
// error.
var (
	Stdout = &File{1, "/dev/stdout"}
	Stderr = &File{2, "/dev/stderr"}
)

// Name returns the name of the file.
func (f *File) Name() string { return f.name }

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
// write system call (syscall/zerrors_linux_amd64.go).
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
