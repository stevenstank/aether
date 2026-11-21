// Corner cases of Go's semantics that compilers get wrong: evaluation
// order, value copies, aliasing, method values binding their receiver,
// defer argument evaluation, overflow, shifts, conversions, interface
// comparison, embedding, and labeled control flow.
package main

import (
	"fmt"
	"math"
	"strings"
)

var trace []string

func t(s string, v int) int {
	trace = append(trace, s)
	return v
}

type pt struct{ x, y int }

func (p pt) sum() int     { return p.x + p.y }
func (p *pt) scale(k int) { p.x, p.y = p.x*k, p.y*k }

type named struct {
	pt
	name string
}

func (n named) String() string { return fmt.Sprintf("%s%v", n.name, n.pt) }

type shadow struct {
	named
	x int
}

func order() {
	trace = nil
	a := []int{0, 0, 0}
	i := 0
	a[t("idx", i)], i = t("v1", 5), t("v2", 2)
	fmt.Println("assign:", a, i, trace)
	trace = nil
	m := map[int]int{}
	m[t("k", 1)] = t("a", 1) + t("b", 2)*t("c", 3)
	fmt.Println("map assign:", m, trace)
	trace = nil
	f := func(a, b, c int) int { return a*100 + b*10 + c }
	fmt.Println("call:", f(t("x", 1), t("y", 2), t("z", 3)), trace)
	x, y := 1, 2
	x, y = y, x
	s := []int{1, 2, 3}
	s[0], s[1], s[2] = s[2], s[0], s[1]
	fmt.Println("swap:", x, y, s)
}

func copies() {
	arr := [3]pt{{1, 2}, {3, 4}, {5, 6}}
	b := arr
	b[0].x = 100
	for _, p := range arr {
		p.x = -1
	}
	for i := range arr {
		arr[i].y *= 10
	}
	fmt.Println("arrays:", arr, b)
	p := pt{2, 3}
	sum := p.sum
	scale := p.scale
	p.x = 50
	scale(2)
	fmt.Println("method values:", sum(), p)
	var fs []func() int
	for i := range 3 {
		fs = append(fs, func() int { return i * i })
	}
	out := []int{}
	for _, f := range fs {
		out = append(out, f())
	}
	fmt.Println("loop vars:", out)
	s := make([]int, 3, 10)
	s2 := append(s, 4)
	s3 := append(s, 5)
	fmt.Println("append alias:", s2[3], s3[3], len(s), cap(s2))
	ov := []int{1, 2, 3, 4, 5, 6}
	n := copy(ov[2:], ov)
	fmt.Println("overlap copy:", n, ov)
	bs := []byte("hello")
	str := string(bs)
	bs[0] = 'J'
	fmt.Println("string copy:", str, string(bs))
	type big struct {
		a [5]int
		s string
	}
	g1 := big{[5]int{1, 2, 3, 4, 5}, "x"}
	g2 := g1
	g2.a[4] = 9
	fmt.Println("struct eq:", g1 == g2, g1 == big{[5]int{1, 2, 3, 4, 5}, "x"})
}

func deferEval() (r int) {
	x := 1
	defer func(v int) { r += v * 100 }(x)
	defer func() { r += x * 10 }()
	x = 7
	p := &pt{1, 1}
	defer t("deferred sum", p.sum())
	defer p.scale(3)
	return x
}

func overflow() {
	var i8 int8 = 127
	i8++
	var u8 uint8 = 0
	u8--
	var i64 int64 = math.MinInt64
	neg := -i64
	d := int64(-1)
	q := i64 / d
	r := i64 % d
	var u32 uint32 = 1 << 31
	u32 <<= 1
	var i32 int32 = math.MaxInt32
	i32 += 2
	sh := 70
	var one uint64 = 1
	var sone int64 = -8
	fmt.Println("wrap:", i8, u8, neg, q, r, u32, i32)
	v200, v300, v70k, big, m1, f39 := uint8(200), 300, 70000, int64(1)<<40+5, int8(-1), 3.9
	fmt.Println("shifts:", one<<sh, one<<63>>63, sone>>sh, sone>>1, v200<<1, int16(m1)<<15)
	fmt.Println("conv:", int8(v300), uint16(v70k), int32(big), uint(m1)%1000, float32(f39) == 3.9, int(-f39), int(f39))
	fmt.Println("div:", -7/2, -7%2, 7/-2, 7%-2, uint8(250)/7)
	nan := math.NaN()
	fm := map[float64]int{nan: 1, nan: 2, 0.0: 3}
	fm[math.Copysign(0, -1)] = 4
	fmt.Println("nan:", len(fm), fm[0], nan == nan, math.Inf(1) > math.MaxFloat64)
}

type I interface{ M() int }
type A int
type B struct{ s []int }

func (a A) M() int { return int(a) }
func (b B) M() int { return len(b.s) }

func ifaces() (err error) {
	var x, y any = A(1), A(1)
	var z any = 1
	fmt.Println("iface eq:", x == y, x == z, any(nil) == nil)
	var i I
	var a *A
	fmt.Println("nil iface:", i == nil, any(a) == nil, any(a) == any((*A)(nil)))
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
		}
	}()
	var u, v I = B{[]int{1}}, B{[]int{1}}
	return fmt.Errorf("unreached %v", u == v)
}

func embedding() {
	s := shadow{named{pt{1, 2}, "n"}, 99}
	fmt.Println("embed:", s.x, s.pt.x, s.named.x, s.sum(), s)
	s.scale(2)
	fmt.Println("promoted ptr method:", s.pt, s.String())
	var st fmt.Stringer = s
	fmt.Println("stringer:", st)
}

func control() {
	var b strings.Builder
outer:
	for i := 0; i < 5; i++ {
		switch {
		case i == 1:
			continue outer
		case i == 3:
			fallthrough
		case i == 4:
			fmt.Fprintf(&b, "[%d ft]", i)
			if i == 4 {
				break outer
			}
		default:
			for j := 0; j < 5; j++ {
				if j == 2 {
					continue outer
				}
				fmt.Fprintf(&b, "%d.%d ", i, j)
			}
		}
	}
	i := 0
loop:
	if i < 3 {
		fmt.Fprintf(&b, "g%d ", i)
		i++
		goto loop
	}
	c := make(chan int, 3)
	c <- 1
	c <- 2
	close(c)
sel:
	for {
		select {
		case v, ok := <-c:
			if !ok {
				break sel
			}
			fmt.Fprintf(&b, "c%d ", v)
		}
	}
	fmt.Println("control:", b.String())
}

func main() {
	order()
	copies()
	fmt.Println("defer eval:", deferEval())
	overflow()
	fmt.Println("ifaces:", ifaces())
	embedding()
	control()
}
