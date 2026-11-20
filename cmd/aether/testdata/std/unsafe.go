package main

import (
	"slices"
	"unsafe"
)

type pair struct {
	a int8
	b int64
	c [3]int16
}

func recovered(f func()) {
	defer func() {
		e := recover().(error)
		println("recovered:", e.Error())
	}()
	f()
}

func main() {
	var p pair
	println(unsafe.Sizeof(p), unsafe.Alignof(p.b), unsafe.Offsetof(p.b), unsafe.Offsetof(p.c), unsafe.Sizeof(""), unsafe.Sizeof([]int{}))

	// unsafe.Pointer conversions and arithmetic.
	arr := [4]int32{10, 20, 30, 40}
	q := unsafe.Pointer(&arr[0])
	third := (*int32)(unsafe.Add(q, 2*unsafe.Sizeof(arr[0])))
	println(*third)
	*third = 33
	println(arr[2], uintptr(unsafe.Pointer(&arr[3]))-uintptr(q), unsafe.Pointer(&arr[1]) == unsafe.Add(q, 4))
	second := (*int32)(unsafe.Pointer(uintptr(q) + 4))
	println(*second)
	var np unsafe.Pointer
	println(np == nil, q != nil)

	// An unsafe.Pointer in an interface is the pointer itself, as gc's.
	var x any = q
	back, ok := x.(unsafe.Pointer)
	println(back == q, ok, x == any(q), x != any(unsafe.Pointer(nil)))
	switch x.(type) {
	case *int32:
		println("not this")
	case unsafe.Pointer:
		println("unsafe.Pointer")
	}

	// String, StringData, Slice, and SliceData.
	bs := []byte("hello")
	s := unsafe.String(unsafe.SliceData(bs), len(bs))
	println(s, unsafe.String(&bs[1], 3), len(unsafe.String(nil, 0)))
	sd := unsafe.StringData(s)
	println(*sd == 'h', unsafe.Slice(sd, 2)[1] == 'e')
	ints := unsafe.Slice(&arr[1], 3)
	println(len(ints), cap(ints), ints[0], ints[2])
	ints[0] = 21
	println(arr[1], unsafe.Slice((*int)(nil), 0) == nil, unsafe.SliceData([]int(nil)) == nil)

	// gc's run-time errors.
	n := -1
	recovered(func() { println(unsafe.String(&bs[0], n)) })
	recovered(func() { println(len(unsafe.String(nil, 1))) })
	recovered(func() { println(len(unsafe.Slice(&arr[0], n))) })
	recovered(func() { println(len(unsafe.Slice((*int)(nil), 2))) })
	recovered(func() { println(len(unsafe.Slice(&arr[0], 1<<62))) })

	// slices with unsafe.
	xs := slices.Insert([]int{1, 4}, 1, 2, 3)
	println(len(xs), xs[1], xs[3])
	ys := slices.Replace(xs, 0, 2, 9)
	println(len(ys), ys[0], ys[1])
}
