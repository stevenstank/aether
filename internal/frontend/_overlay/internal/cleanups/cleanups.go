// Package runtime, at internal/cleanups, is what net and internal/poll
// use of gc's runtime in Aether (D-056): with GOOS and KeepAlive, the
// finalizers and cleanups that close a descriptor or a pipe nobody
// closed. Aether's collector runs none, which gc does not promise to
// either, so such a descriptor stays open until the program exits. A
// program's own runtime.SetFinalizer and AddCleanup stay unsupported:
// it may depend on them running.
package runtime

import rt "runtime"

const GOOS = rt.GOOS

func KeepAlive(x any) { rt.KeepAlive(x) }

// SetFinalizer does nothing: the finalizer never runs.
func SetFinalizer(obj any, finalizer any) {}

// Cleanup is gc's handle of a cleanup that never runs.
type Cleanup struct{}

// AddCleanup returns a Cleanup that never runs.
func AddCleanup[T, S any](ptr *T, cleanup func(S), arg S) Cleanup { return Cleanup{} }

// Stop does nothing: the cleanup never runs.
func (c Cleanup) Stop() {}
