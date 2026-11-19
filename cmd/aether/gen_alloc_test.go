package main

import (
	"fmt"
	"math/rand/v2"
)

// Generated allocation programs (Phase 9, D-046).
//
// generateAllocs writes a program of its own, from its own random stream,
// for the garbage collector: a loop of a few thousand iterations runs
// random statements that allocate and drop binary trees, linked lists of
// structs with arrays, map entries holding slices of pointers, closures
// capturing pointers, interfaces holding pointers and strings, strings
// built at run time, large slices, tails of slices, and pointers to arrays
// inside structs, driven by a pseudo-random x the program computes. Every
// so often, and at the end, it prints checksums that read everything still
// reachable, so that an object freed while reachable, which the
// collector's stress mode overwrites, changes the output. The trees, the
// map, the tails, and the pointers to arrays are package-level variables.
// Nothing printed depends on map order.
//
// V-4: each statement makes at most one call of a function that changes
// anything, and its other operands are variables that call cannot assign.

// generateAllocs returns the allocation program for seed.
func generateAllocs(seed uint64) string {
	g := &mgen{r: rand.New(rand.NewPCG(seed, 0xa110c))}
	g.line(0, "package main\n")
	g.line(0, "type T struct {\n\tl, r *T\n\tk    int\n\ts    string\n\txs   []int\n}\n")
	g.line(0, "type L struct {\n\tnext *L\n\tv    [3]int\n}\n")
	g.line(0, "func insert(t *T, k int) *T {\n\tif t == nil {\n\t\treturn &T{k: k, s: string(rune('a' + k%%26))}\n\t}\n\tif k < t.k {\n\t\tt.l = insert(t.l, k)\n\t} else if k > t.k {\n\t\tt.r = insert(t.r, k)\n\t} else {\n\t\tt.xs = append(t.xs, k)\n\t}\n\treturn t\n}\n")
	g.line(0, "func sum(t *T) int {\n\tif t == nil {\n\t\treturn 0\n\t}\n\tn := t.k*3 + len(t.s) + len(t.xs)\n\tfor i := 0; i < len(t.xs); i++ {\n\t\tn += t.xs[i]\n\t}\n\treturn n + sum(t.l) + sum(t.r)\n}\n")
	g.line(0, "func lsum(l *L) int {\n\tn := 0\n\tfor ; l != nil; l = l.next {\n\t\tn += l.v[0] + l.v[1]*7 + l.v[2]\n\t}\n\treturn n\n}\n")
	g.line(0, "func check(roots [4]*T, lists [3]*L, m map[int][]*T, fs []func() int, is []any, strs []string) int {")
	g.line(1, "n := 0")
	g.line(1, "for i := 0; i < 4; i++ {\n\t\tn += sum(roots[i]) * (i + 1)\n\t}")
	g.line(1, "for i := 0; i < 3; i++ {\n\t\tn += lsum(lists[i]) * (i + 5)\n\t}")
	g.line(1, "for k := 0; k < 16; k++ {\n\t\tts := m[k]\n\t\tfor i := 0; i < len(ts); i++ {\n\t\t\tn += sum(ts[i]) + k\n\t\t}\n\t}")
	g.line(1, "for i := 0; i < len(fs); i++ {\n\t\tn += fs[i]()\n\t}")
	g.line(1, "for i := 0; i < len(is); i++ {\n\t\tswitch v := is[i].(type) {\n\t\tcase *T:\n\t\t\tn += sum(v)\n\t\tcase string:\n\t\t\tn += len(v)\n\t\tcase *L:\n\t\t\tn += lsum(v)\n\t\t}\n\t}")
	g.line(1, "for i := 0; i < len(strs); i++ {\n\t\tn += len(strs[i])\n\t\tif len(strs[i]) > 0 {\n\t\t\tn += int(strs[i][0])\n\t\t}\n\t}")
	g.line(1, "for i := 0; i < len(tails); i++ {\n\t\tn += len(tails[i]) + tails[i][len(tails[i])-1]\n\t}")
	g.line(1, "for i := 0; i < len(inner); i++ {\n\t\tn += inner[i][0] + inner[i][1]*3 + inner[i][2]\n\t}")
	g.line(1, "return n")
	g.line(0, "}\n")
	// The trees, the map, the tails, and the arrays inside structs are
	// package-level variables, kept alive through .bss only. A tail of a
	// slice and a pointer to a struct's field point inside the object
	// they keep alive.
	g.line(0, "var roots [4]*T\n")
	g.line(0, "var m = map[int][]*T{}\n")
	g.line(0, "var tails [][]int\n")
	g.line(0, "var inner []*[3]int\n")
	g.line(0, "func main() {")
	g.line(1, "var lists [3]*L")
	g.line(1, "var fs []func() int")
	g.line(1, "var is []any")
	g.line(1, "var strs []string")
	g.line(1, "x, acc := %d, 0", 1+g.pick(1000))
	g.line(1, "for it := 0; it < %d; it++ {", 1000+g.pick(3000))
	g.line(2, "x = (x*1103515245 + 12345) & 0x7fffffff")
	for range 4 + g.pick(8) {
		g.allocStmt()
	}
	g.line(2, "if it%%%d == 0 {\n\t\t\tprintln(it, check(roots, lists, m, fs, is, strs), acc)\n\t\t}", 200+g.pick(800))
	g.line(1, "}")
	g.line(1, "println(check(roots, lists, m, fs, is, strs), acc, len(m), len(fs), len(is), len(strs))")
	g.line(0, "}")
	return g.b.String()
}

// allocStmt emits a statement of the loop's body, at depth 2, under a
// condition on x so that each runs on some iterations only.
func (g *mgen) allocStmt() {
	i, j := g.pick(4), g.pick(3)
	var s string
	switch g.pick(14) {
	case 12:
		s = fmt.Sprintf("s := make([]int, 40+x%%200)\n\t\t\ts[len(s)-1] = it\n\t\t\ttails = append(tails, s[1+x%%39:])\n\t\t\tif len(tails) > %d {\n\t\t\t\ttails = tails[1:]\n\t\t\t}", 5+g.pick(40))
	case 13:
		s = fmt.Sprintf("pl := &L{nil, [3]int{it, x %% 9, %d}}\n\t\t\tinner = append(inner, &pl.v)\n\t\t\tif len(inner) > %d {\n\t\t\t\tinner = inner[1:]\n\t\t\t}", i, 5+g.pick(40))
	case 0, 1:
		s = fmt.Sprintf("roots[%d] = insert(roots[%d], x%%%d)", i, i, 50+g.pick(500))
	case 2:
		s = fmt.Sprintf("roots[%d] = nil", i)
	case 3:
		s = fmt.Sprintf("lists[%d] = &L{lists[%d], [3]int{x %% 1000, it, %d}}", j, j, i)
	case 4:
		s = fmt.Sprintf("if lists[%d] != nil {\n\t\t\t\tlists[%d] = lists[%d].next\n\t\t\t}", j, j, j)
	case 5:
		s = fmt.Sprintf("m[x%%16] = append(m[x%%16], roots[%d])", i)
	case 6:
		s = "delete(m, x%16)"
	case 7:
		s = fmt.Sprintf("p := &L{lists[%d], [3]int{x %% 77, %d, it %% 5}}\n\t\t\tfs = append(fs, func() int { return p.v[0] + lsum(p.next) %% 1000 })\n\t\t\tif len(fs) > %d {\n\t\t\t\tfs = fs[%d:]\n\t\t\t}", j, i, 8+g.pick(32), 1+g.pick(8))
	case 8:
		s = fmt.Sprintf("is = append(is, roots[%d], lists[%d], string(rune('A'+x%%26)))\n\t\t\tif len(is) > %d {\n\t\t\t\tis = is[3:]\n\t\t\t}", i, j, 30+g.pick(60))
	case 9:
		s = fmt.Sprintf("t := \"\"\n\t\t\tif len(strs) > 0 {\n\t\t\t\tt = strs[x%%len(strs)]\n\t\t\t}\n\t\t\tif len(t) > 40 {\n\t\t\t\tt = t[%d:]\n\t\t\t}\n\t\t\tstrs = append(strs, t+string(rune('a'+x%%26)))\n\t\t\tif len(strs) > %d {\n\t\t\t\tstrs = strs[1:]\n\t\t\t}", 1+g.pick(20), 10+g.pick(50))
	case 10:
		s = fmt.Sprintf("big := make([]int, x%%%d+1)\n\t\t\tbig[x%%len(big)] = it\n\t\t\tacc += big[x%%len(big)] + len(big)", 1000+g.pick(30000))
	default:
		s = fmt.Sprintf("if roots[%d] != nil {\n\t\t\t\troots[%d] = roots[%d].l\n\t\t\t}", i, i, i)
	}
	g.line(2, "if x%%%d < %d {\n\t\t\t%s\n\t\t}", 10, 1+g.pick(9), s)
}
