//go:build !(linux && amd64)

package interp

import "errors"

// errNoHost reports that the interpreter makes a program's system calls
// through the host's, which must be Linux x86-64's (D-051).
var errNoHost = errors.New("system calls are emulated only on a linux/amd64 host")

func (p *process) syscall(*memory, [7]int64) (int64, error) { return 0, errNoHost }

func hostRead(int, []byte) int64  { return -9 }
func hostWrite(int, []byte) int64 { return -9 }

func (p *process) closeFiles() {}
