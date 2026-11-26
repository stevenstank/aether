// gc's rlimit.go without its init, which raises the soft limit of open
// files at startup: an Aether program keeps the limit it was started
// with.

package syscall

import "sync/atomic"

// origRlimitNofile is gc's: nil, the limit never changed at startup.
var origRlimitNofile atomic.Pointer[Rlimit]

func Setrlimit(resource int, rlim *Rlimit) error {
	return setrlimit(resource, rlim)
}
