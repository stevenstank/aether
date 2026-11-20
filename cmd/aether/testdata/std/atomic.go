package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	var i32 int32
	var i64 int64
	var u32 uint32
	var u64 uint64
	var up uintptr
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt32(&i32, 2)
			atomic.AddInt64(&i64, -3)
			atomic.AddUint32(&u32, 1)
			atomic.AddUint64(&u64, ^uint64(0)) // subtracts 1
			atomic.AddUintptr(&up, 5)
		}()
	}
	wg.Wait()
	println(atomic.LoadInt32(&i32), atomic.LoadInt64(&i64), atomic.LoadUint32(&u32), atomic.LoadUint64(&u64), atomic.LoadUintptr(&up))

	// Each result is printed before the variable it changed.
	r := atomic.SwapInt32(&i32, 7)
	println(r, i32)
	s1, s2 := atomic.CompareAndSwapInt32(&i32, 6, 9), atomic.CompareAndSwapInt32(&i32, 7, 9)
	println(s1, s2, i32)
	s1 = atomic.CompareAndSwapInt64(&i64, -150, 1)
	println(s1, i64)
	s1 = atomic.CompareAndSwapUint32(&u32, 1, 2)
	r64 := atomic.SwapUint64(&u64, 1<<63)
	println(s1, r64, u64)
	atomic.StoreInt64(&i64, -1)
	atomic.StoreUint32(&u32, 0xf0f0)
	ru := atomic.AndUint32(&u32, 0xff00)
	println(ru, u32)
	ru = atomic.OrUint32(&u32, 0x000f)
	println(ru, u32)
	r64s := atomic.AndInt64(&i64, 6)
	println(r64s, i64)
	r = atomic.OrInt32(&i32, 16)
	println(r, i32)
	var max32 int32 = 1<<31 - 1
	println(atomic.AddInt32(&max32, 1)) // wraps

	var a atomic.Int32
	var b atomic.Int64
	var c atomic.Uint32
	var d atomic.Uint64
	var e atomic.Uintptr
	var f atomic.Bool
	println(a.Add(5), a.Load(), a.Swap(3), a.CompareAndSwap(3, 4), a.Load(), a.And(6), a.Or(1), a.Load())
	b.Store(-10)
	println(b.Add(-5), b.Swap(2), b.CompareAndSwap(1, 0), b.Load(), b.And(3), b.Or(8), b.Load())
	c.Store(1)
	println(c.Add(^uint32(0)), c.Swap(9), c.CompareAndSwap(9, 10), c.Load(), c.And(2), c.Or(5), c.Load())
	println(d.Add(1<<40), d.Swap(3), d.CompareAndSwap(3, 4), d.Load(), d.And(5), d.Or(2), d.Load())
	println(e.Add(4), e.Swap(1), e.CompareAndSwap(2, 3), e.Load(), e.And(1), e.Or(6), e.Load())
	println(f.Load(), f.Swap(true), f.CompareAndSwap(false, true), f.CompareAndSwap(true, false), f.Load())
	f.Store(true)
	println(f.Load())

	var p *int32
	defer func() { println("recovered:", recover() != nil) }()
	atomic.AddInt32(p, 1)
}
