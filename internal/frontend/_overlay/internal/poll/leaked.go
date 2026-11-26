package poll

// Leaked is what closing a collected FD needs, for the cleanup package net
// attaches to its descriptors (D-057), which must not reach the FD.
type Leaked struct {
	sysfd int
	ctx   uintptr
}

// Leaked returns fd's Leaked.
func (fd *FD) Leaked() Leaked { return Leaked{fd.Sysfd, fd.pd.runtimeCtx} }

// Close takes the descriptor of an FD nobody references any more off the
// poller and closes it, as FD.Close would have: nothing can be using it.
func (l Leaked) Close() {
	if l.ctx != 0 {
		if pd := lookupPD(l.ctx); !pd.closing {
			aetherPollUnblock(l.ctx)
		}
		aetherPollClose(l.ctx)
	}
	if l.sysfd >= 0 {
		CloseFunc(l.sysfd)
	}
}
