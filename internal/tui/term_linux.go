package tui

import (
	"os"
	"syscall"
	"unsafe"
)

// This is the only file of the package that uses syscall, and only for the
// terminal ioctls (D-061); TestNoProcessStartOutsideToolchain enforces it.

// rawTerm is a terminal in raw mode; restore returns it to its saved state.
type rawTerm struct {
	fd    uintptr
	saved syscall.Termios
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	return ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

// makeRaw puts the terminal f into raw mode: no echo, no line editing, no
// signals from keys, and reads that return as soon as one byte arrives.
func makeRaw(f *os.File) (*rawTerm, error) {
	r := &rawTerm{fd: f.Fd()}
	if err := ioctl(r.fd, syscall.TCGETS, unsafe.Pointer(&r.saved)); err != nil {
		return nil, err
	}
	t := r.saved
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	if err := ioctl(r.fd, syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rawTerm) restore() {
	ioctl(r.fd, syscall.TCSETS, unsafe.Pointer(&r.saved))
}

// termSize returns the size of the terminal f in columns and rows.
func termSize(f *os.File) (w, h int, err error) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}

// resizeSignal is delivered when the terminal's size changes.
var resizeSignal os.Signal = syscall.SIGWINCH

// quitSignals end the TUI as the q key does. In raw mode ctrl+c is a key,
// so SIGINT comes only from kill.
var quitSignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT}
