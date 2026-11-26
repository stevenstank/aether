package sync

// Cond is a condition variable, a rendezvous point for goroutines waiting
// for or announcing an event (D-057). As the rest of Aether's sync, it
// relies on goroutines switching only when one blocks or yields (D-048).
type Cond struct {
	// L is held while observing or changing the condition.
	L Locker

	waiters []chan struct{} // the goroutines in Wait, in order
}

// NewCond returns a new Cond with Locker l.
func NewCond(l Locker) *Cond {
	return &Cond{L: l}
}

// Wait unlocks c.L, waits until Signal or Broadcast wakes it, and locks
// c.L again before returning.
func (c *Cond) Wait() {
	ch := make(chan struct{}, 1)
	c.waiters = append(c.waiters, ch)
	c.L.Unlock()
	<-ch
	c.L.Lock()
}

// Signal wakes the goroutine that has waited longest on c, if there is
// one.
func (c *Cond) Signal() {
	if len(c.waiters) > 0 {
		ch := c.waiters[0]
		c.waiters[0] = nil
		c.waiters = c.waiters[1:]
		ch <- struct{}{}
	}
}

// Broadcast wakes all goroutines waiting on c.
func (c *Cond) Broadcast() {
	ws := c.waiters
	c.waiters = nil
	for _, ch := range ws {
		ch <- struct{}{}
	}
}
