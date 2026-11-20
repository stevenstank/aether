package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Generated goroutine and channel programs (Phase 10 increment 10.4,
// D-048).
//
// generateChans writes a program of its own, from its own random stream.
// main runs a random sequence of blocks, each in its own scope: pipelines
// of stages over buffered and unbuffered channels; worker pools that a
// closer goroutine ends; select loops over producers that drop closed
// channels; a server goroutine answering requests on reply channels
// carried in a struct, stopped through a select; buffered channels and
// non-blocking selects that only main uses; and a goroutine that recovers
// its own panic and sends the value. Elements are ints, strings, structs,
// and arrays. Only main prints, and only what does not depend on the
// schedule: values received in an order the program fixes, or sums,
// counts, and xors (V-10).
//
// About one program in four ends in a deadlock, a close of a closed
// channel, or a send on a closed channel in a goroutine while main waits.

// chanOps are the stages' operations on x, an int.
var chanOps = []string{"x*3 + 1", "x ^ 5", "x % 7", "-x", "x*x - x", "x/2 + 9"}

type cgen struct {
	r *rand.Rand
	b strings.Builder
}

func (g *cgen) pick(n int) int { return g.r.IntN(n) }

func (g *cgen) line(indent int, format string, args ...any) {
	g.b.WriteString(strings.Repeat("\t", indent))
	fmt.Fprintf(&g.b, format, args...)
	g.b.WriteByte('\n')
}

// capacity returns a channel capacity: often 0.
func (g *cgen) capacity() int { return []int{0, 0, 1, 2, 5}[g.pick(5)] }

// generateChans returns the goroutine and channel program for seed.
func generateChans(seed uint64) string {
	g := &cgen{r: rand.New(rand.NewPCG(seed, 0xc4a7))}
	g.line(0, "package main\n")
	g.line(0, "type P struct {\n\ta int8\n\tb int\n\ts string\n}\n")
	g.line(0, "type Req struct {\n\tx     int\n\treply chan int\n}\n")
	g.line(0, "func main() {")
	for range 2 + g.pick(5) {
		g.line(1, "{")
		[]func(){g.pipeline, g.pool, g.selectFan, g.server, g.queue, g.nonblocking, g.recovers}[g.pick(7)]()
		g.line(1, "}")
	}
	switch g.pick(12) {
	case 0:
		g.line(1, "println(\"waiting\")\n\t<-make(chan int)")
	case 1:
		g.line(1, "c := make(chan P, 1)\n\tclose(c)\n\tclose(c)")
	case 2:
		g.line(1, "c := make(chan string)\n\tclose(c)\n\tgo func() { c <- \"x\" }()\n\t<-make(chan bool)")
	}
	g.line(0, "}")
	return g.b.String()
}

// pipeline: a source, 1 to 3 stages, and main printing what arrives.
func (g *cgen) pipeline() {
	n := 1 + g.pick(40)
	g.line(2, "src := make(chan int, %d)", g.capacity())
	g.line(2, "go func() {\n\t\t\tfor i := 0; i < %d; i++ {\n\t\t\t\tsrc <- i * %d\n\t\t\t}\n\t\t\tclose(src)\n\t\t}()", n, 1+g.pick(5))
	g.line(2, "var in <-chan int = src")
	for k := range 1 + g.pick(3) {
		g.line(2, "s%d := make(chan int, %d)", k, g.capacity())
		g.line(2, "go func(in <-chan int, out chan<- int) {\n\t\t\tfor x := range in {\n\t\t\t\tout <- %s\n\t\t\t}\n\t\t\tclose(out)\n\t\t}(in, s%d)", chanOps[g.pick(len(chanOps))], k)
		g.line(2, "in = s%d", k)
	}
	g.line(2, "sum, cnt := 0, 0")
	g.line(2, "for x := range in {\n\t\t\tsum = sum*31 + x\n\t\t\tcnt++\n\t\t}")
	g.line(2, "println(\"pipe\", sum, cnt)")
}

// pool: workers share a jobs channel; a closer goroutine closes results
// once every worker is done; main sums what arrives in any order.
func (g *cgen) pool() {
	w, n := 1+g.pick(6), g.pick(60)
	g.line(2, "jobs, results, done := make(chan int, %d), make(chan P, %d), make(chan bool)", g.capacity(), g.capacity())
	g.line(2, "go func() {\n\t\t\tfor i := 1; i <= %d; i++ {\n\t\t\t\tjobs <- i\n\t\t\t}\n\t\t\tclose(jobs)\n\t\t}()", n)
	g.line(2, "for w := 0; w < %d; w++ {", w)
	g.line(3, "go func(w int) {\n\t\t\t\tfor j := range jobs {\n\t\t\t\t\tresults <- P{int8(j %% 100), j * %s, string(rune('a' + j%%26))}\n\t\t\t\t}\n\t\t\t\tdone <- true\n\t\t\t}(w)", []string{"j", "3", "(j + 1)"}[g.pick(3)])
	g.line(2, "}")
	g.line(2, "go func() {\n\t\t\tfor i := 0; i < %d; i++ {\n\t\t\t\t<-done\n\t\t\t}\n\t\t\tclose(results)\n\t\t}()", w)
	g.line(2, "sum, x, n, l := 0, 0, 0, 0")
	g.line(2, "for r := range results {\n\t\t\tsum += r.b + int(r.a)\n\t\t\tx ^= r.b\n\t\t\tn++\n\t\t\tl += len(r.s) + int(r.s[0])\n\t\t}")
	g.line(2, "println(\"pool\", sum, x, n, l)")
}

// selectFan: two producers of different element types, received by a
// select loop that drops each channel once it is closed.
func (g *cgen) selectFan() {
	g.line(2, "a, b := make(chan int, %d), make(chan [2]int, %d)", g.capacity(), g.capacity())
	g.line(2, "go func() {\n\t\t\tfor i := 0; i < %d; i++ {\n\t\t\t\ta <- i * %d\n\t\t\t}\n\t\t\tclose(a)\n\t\t}()", g.pick(50), 1+g.pick(9))
	g.line(2, "go func() {\n\t\t\tfor i := 0; i < %d; i++ {\n\t\t\t\tb <- [2]int{i, -i * %d}\n\t\t\t}\n\t\t\tclose(b)\n\t\t}()", g.pick(50), 1+g.pick(9))
	g.line(2, "sa, sb, n := 0, 0, 0")
	g.line(2, "for a != nil || b != nil {")
	g.line(3, "select {")
	g.line(3, "case v, ok := <-a:\n\t\t\t\tif !ok {\n\t\t\t\t\ta = nil\n\t\t\t\t\tcontinue\n\t\t\t\t}\n\t\t\t\tsa += v")
	g.line(3, "case v, ok := <-b:\n\t\t\t\tif !ok {\n\t\t\t\t\tb = nil\n\t\t\t\t\tbreak\n\t\t\t\t}\n\t\t\t\tsb += v[0] * v[1]")
	g.line(3, "}")
	g.line(3, "n++")
	g.line(2, "}")
	g.line(2, "println(\"sel\", sa, sb, n)")
}

// server: a goroutine answering requests until main stops it.
func (g *cgen) server() {
	g.line(2, "reqs, quit := make(chan Req, %d), make(chan bool)", g.capacity())
	g.line(2, "go func() {\n\t\t\tfor {\n\t\t\t\tselect {\n\t\t\t\tcase r := <-reqs:\n\t\t\t\t\tr.reply <- r.x*%d + %d\n\t\t\t\tcase <-quit:\n\t\t\t\t\tquit <- true\n\t\t\t\t\treturn\n\t\t\t\t}\n\t\t\t}\n\t\t}()", 1+g.pick(9), g.pick(100))
	g.line(2, "t := 0")
	g.line(2, "for i := 0; i < %d; i++ {\n\t\t\trc := make(chan int, %d)\n\t\t\treqs <- Req{i, rc}\n\t\t\tt = t*7 + <-rc\n\t\t}", g.pick(30), g.capacity())
	g.line(2, "quit <- true\n\t\t<-quit")
	g.line(2, "println(\"req\", t)")
}

// queue: a buffered channel only main uses, drained after close.
func (g *cgen) queue() {
	c := 1 + g.pick(8)
	g.line(2, "q := make(chan string, %d)", c)
	g.line(2, "for i := 0; i < %d; i++ {\n\t\t\tq <- string(rune('a' + i))\n\t\t}", 1+g.pick(c))
	g.line(2, "println(\"q\", len(q), cap(q))")
	g.line(2, "s := <-q")
	g.line(2, "close(q)")
	g.line(2, "for v := range q {\n\t\t\ts += v\n\t\t}")
	g.line(2, "v, ok := <-q")
	g.line(2, "println(\"q\", s, len(v), ok, len(q))")
}

// nonblocking: selects with default on channels only main uses.
func (g *cgen) nonblocking() {
	g.line(2, "c := make(chan int, %d)", g.pick(3))
	g.line(2, "n := 0")
	g.line(2, "for i := 0; i < %d; i++ {\n\t\t\tselect {\n\t\t\tcase c <- i:\n\t\t\t\tn++\n\t\t\tdefault:\n\t\t\t\tn += 100\n\t\t\t}\n\t\t}", g.pick(6))
	g.line(2, "var nc chan int")
	g.line(2, "select {\n\t\tcase <-nc:\n\t\t\tn = -1\n\t\tcase v := <-c:\n\t\t\tn += v * 1000\n\t\tdefault:\n\t\t\tn += 7\n\t\t}")
	g.line(2, "println(\"nb\", n, len(c))")
}

// recovers: a goroutine recovers its own panic and sends the value.
func (g *cgen) recovers() {
	g.line(2, "r := make(chan string)")
	g.line(2, "go func() {\n\t\t\tdefer func() {\n\t\t\t\tr <- recover().(string)\n\t\t\t}()\n\t\t\tpanic(\"p%d\")\n\t\t}()", g.pick(100))
	g.line(2, "println(\"rec\", <-r)")
}
