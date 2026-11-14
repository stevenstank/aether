// aether:expect unsupported frame-size

package main

// Each copy of a 16 MiB array is a variable and a temporary in main's frame:
// 40 of them may need more than the 512 MiB the gate allows.

type Big [1 << 21]int64

func main() {
	var a0 Big
	a0[0] = 1
	a1 := a0
	a2 := a1
	a3 := a2
	a4 := a3
	a5 := a4
	a6 := a5
	a7 := a6
	a8 := a7
	a9 := a8
	a10 := a9
	a11 := a10
	a12 := a11
	a13 := a12
	a14 := a13
	a15 := a14
	a16 := a15
	a17 := a16
	a18 := a17
	a19 := a18
	a20 := a19
	a21 := a20
	a22 := a21
	a23 := a22
	a24 := a23
	a25 := a24
	a26 := a25
	a27 := a26
	a28 := a27
	a29 := a28
	a30 := a29
	a31 := a30
	a32 := a31
	a33 := a32
	a34 := a33
	a35 := a34
	a36 := a35
	a37 := a36
	a38 := a37
	a39 := a38
	println(a39[0])
}
