package os

import (
	"internal/oserror"
	"syscall"
)

// Portable analogs of some common system call errors: gc's io/fs errors,
// which are internal/oserror's, as syscall.Errno's Is knows them (D-054).
var (
	ErrInvalid    = oserror.ErrInvalid
	ErrPermission = oserror.ErrPermission
	ErrExist      = oserror.ErrExist
	ErrNotExist   = oserror.ErrNotExist
	ErrClosed     = oserror.ErrClosed
)

// PathError records an error and the operation and file path that caused
// it.
type PathError struct {
	Op   string
	Path string
	Err  error
}

func (e *PathError) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }

func (e *PathError) Unwrap() error { return e.Err }

// Timeout reports whether this error represents a timeout.
func (e *PathError) Timeout() bool {
	t, ok := e.Err.(timeout)
	return ok && t.Timeout()
}

type timeout interface {
	Timeout() bool
}

// LinkError records an error during a link or symlink or rename system
// call and the paths that caused it.
type LinkError struct {
	Op  string
	Old string
	New string
	Err error
}

func (e *LinkError) Error() string {
	return e.Op + " " + e.Old + " " + e.New + ": " + e.Err.Error()
}

func (e *LinkError) Unwrap() error { return e.Err }

// SyscallError records an error from a specific system call.
type SyscallError struct {
	Syscall string
	Err     error
}

func (e *SyscallError) Error() string { return e.Syscall + ": " + e.Err.Error() }

func (e *SyscallError) Unwrap() error { return e.Err }

// Timeout reports whether this error represents a timeout.
func (e *SyscallError) Timeout() bool {
	t, ok := e.Err.(timeout)
	return ok && t.Timeout()
}

// NewSyscallError returns, as an error, a new SyscallError with the given
// system call name and error details, or nil if err is nil.
func NewSyscallError(syscall string, err error) error {
	if err == nil {
		return nil
	}
	return &SyscallError{syscall, err}
}

// IsExist reports whether err, not unwrapped beyond a PathError,
// LinkError, or SyscallError, says a file or directory already exists.
func IsExist(err error) bool { return underlyingErrorIs(err, ErrExist) }

// IsNotExist reports whether err, as IsExist, says a file or directory
// does not exist.
func IsNotExist(err error) bool { return underlyingErrorIs(err, ErrNotExist) }

// IsPermission reports whether err, as IsExist, says permission is
// denied.
func IsPermission(err error) bool { return underlyingErrorIs(err, ErrPermission) }

// IsTimeout reports whether err, as IsExist, says a timeout occurred.
func IsTimeout(err error) bool {
	terr, ok := underlyingError(err).(timeout)
	return ok && terr.Timeout()
}

func underlyingErrorIs(err, target error) bool {
	err = underlyingError(err)
	if err == target {
		return true
	}
	e, ok := err.(syscall.Errno)
	return ok && e.Is(target)
}

func underlyingError(err error) error {
	switch err := err.(type) {
	case *PathError:
		return err.Err
	case *LinkError:
		return err.Err
	case *SyscallError:
		return err.Err
	}
	return err
}

// The Linux error numbers package os uses.
const (
	eperm     = 1
	enoent    = 2
	eintr     = 4
	eacces    = 13
	eexist    = 17
	enotdir   = 20
	einval    = 22
	enotempty = 39
)

func itoa(n int) string {
	neg := n < 0
	u := uint(n)
	if neg {
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
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
