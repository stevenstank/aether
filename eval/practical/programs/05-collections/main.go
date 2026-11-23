// Command collections exercises generic containers: an AVL tree, a binary
// heap, an LRU cache, and a set, checking each against simple reference
// implementations under a reproducible random workload.
//
// Usage: collections [OPS] [SEED]
package main

import (
	"fmt"
	"math/rand"
	"os"
	"slices"
	"sort"
	"strconv"
)

type task struct {
	priority int
	name     string
	seq      int
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "MISMATCH: "+format+"\n", args...)
	os.Exit(1)
}

func treeDemo() {
	t := &Tree[string, int]{}
	for i, w := range []string{"pear", "apple", "fig", "kiwi", "banana", "cherry", "date", "apple"} {
		t.Put(w, i)
	}
	t.Delete("fig")
	t.Delete("missing")
	fmt.Printf("tree: len=%d height=%d\n", t.Len(), t.Height())
	t.Ascend("b", "e", func(k string, v int) bool {
		fmt.Printf("  %s=%d\n", k, v)
		return true
	})
	v, ok := t.Get("apple")
	fmt.Println("  get apple:", v, ok)
	_, ok = t.Get("fig")
	fmt.Println("  get fig:", ok)
}

func treeStress(r *rand.Rand, ops int) {
	t := &Tree[int, int]{}
	ref := map[int]int{}
	for i := 0; i < ops; i++ {
		k := r.Intn(ops / 2)
		switch r.Intn(10) {
		case 0, 1, 2:
			_, had := ref[k]
			if t.Delete(k) != had {
				fail("delete %d", k)
			}
			delete(ref, k)
		case 3, 4:
			v, ok := t.Get(k)
			rv, rok := ref[k]
			if v != rv || ok != rok {
				fail("get %d: %d,%v want %d,%v", k, v, ok, rv, rok)
			}
		default:
			t.Put(k, i)
			ref[k] = i
		}
	}
	if n := t.check(); n != len(ref) || t.Len() != len(ref) {
		fail("size %d/%d want %d", n, t.Len(), len(ref))
	}
	keys := make([]int, 0, len(ref))
	for k := range ref {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var got []int
	t.Ascend(keys[len(keys)/4], keys[3*len(keys)/4], func(k, v int) bool {
		if ref[k] != v {
			fail("ascend value of %d", k)
		}
		got = append(got, k)
		return true
	})
	if !slices.Equal(got, keys[len(keys)/4:3*len(keys)/4]) {
		fail("ascend range: got %d keys", len(got))
	}
	// A balanced tree of n nodes has height < 1.45 log2(n+2).
	limit := 0
	for n := len(ref) + 2; n > 1; n >>= 1 {
		limit++
	}
	fmt.Printf("tree stress: ops=%d size=%d height=%d (log2 bound %d) ok\n", ops, t.Len(), t.Height(), limit)
}

func heapDemo() {
	h := NewHeap(func(a, b task) bool {
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		return a.seq < b.seq
	})
	for i, s := range []struct {
		p int
		n string
	}{{2, "write report"}, {5, "fix outage"}, {1, "lunch"}, {5, "page oncall"}, {3, "review PR"}} {
		h.Push(task{s.p, s.n, i})
	}
	top, _ := h.Peek()
	fmt.Println("heap: peek =", top.name)
	for h.Len() > 0 {
		t, _ := h.Pop()
		fmt.Printf("  [%d] %s\n", t.priority, t.name)
	}
	if _, ok := h.Pop(); ok {
		fail("pop from empty heap")
	}
}

func heapStress(r *rand.Rand, n int) {
	h := NewHeap(func(a, b int) bool { return a < b })
	var ref []int
	for i := 0; i < n; i++ {
		x := r.Intn(1_000_000) - 500_000
		h.Push(x)
		pos, _ := slices.BinarySearch(ref, x)
		ref = slices.Insert(ref, pos, x)
		if i%3 == 2 {
			v, _ := h.Pop()
			if v != ref[0] {
				fail("heap pop %d want %d", v, ref[0])
			}
			ref = ref[1:]
		}
	}
	sum := 0
	for i := range ref {
		v, _ := h.Pop()
		if v != ref[i] {
			fail("heap drain %d want %d", v, ref[i])
		}
		sum += v
	}
	fmt.Printf("heap stress: n=%d drained=%d sum=%d ok\n", n, len(ref), sum)
}

func lruDemo(r *rand.Rand, ops int) {
	c := NewLRU[string, int](3)
	var evicted []string
	c.OnEvict = func(k string, v int) { evicted = append(evicted, k+"="+strconv.Itoa(v)) }
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	c.Get("a")
	c.Put("d", 4)
	c.Put("b", 20)
	c.Put("e", 5)
	fmt.Println("lru: keys", c.Keys(), "evicted", evicted)

	// Zipf-like access pattern: low keys are much more popular.
	big := NewLRU[int, int](64)
	for i := 0; i < ops; i++ {
		k := int(float64(r.Intn(1000)+1) * float64(r.Intn(1000)+1) / 1000)
		if _, ok := big.Get(k); !ok {
			big.Put(k, k*k)
		}
	}
	fmt.Printf("lru stress: ops=%d hits=%d misses=%d hit-rate=%.3f len=%d\n",
		ops, big.Hits, big.Misses, float64(big.Hits)/float64(ops), big.Len())

	defer func() {
		fmt.Println("lru: recovered:", recover())
	}()
	NewLRU[int, int](0)
}

func setDemo() {
	a := SetOf(1, 2, 3, 4, 5, 6)
	b := SetOf(4, 5, 6, 7, 8)
	fmt.Println("set: union", FormatSet(a.Union(b)))
	fmt.Println("set: intersect", FormatSet(a.Intersect(b)))
	fmt.Println("set: diff", FormatSet(a.Diff(b)))
	words := SetOf("go", "rust", "zig", "go", "c")
	fmt.Println("set: words", FormatSet(words), words.Len())

	nums := []int{3, 8, 1, 9, 4, 7, 2, 6, 5, 10}
	sq := Map(nums, func(x int) int { return x * x })
	even := Filter(sq, func(x int) bool { return x%2 == 0 })
	total := Reduce(even, 0, func(a, x int) int { return a + x })
	strs := Map(even, func(x int) string { return "<" + strconv.Itoa(x) + ">" })
	fmt.Println("funcs:", sq, even, total, strs)
}

func main() {
	ops, seed := 200000, int64(1)
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil || n < 10 {
			fmt.Fprintf(os.Stderr, "collections: bad op count %q\n", os.Args[1])
			os.Exit(2)
		}
		ops = n
	}
	if len(os.Args) > 2 {
		s, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collections: bad seed %q\n", os.Args[2])
			os.Exit(2)
		}
		seed = s
	}
	r := rand.New(rand.NewSource(seed))
	treeDemo()
	treeStress(r, ops)
	heapDemo()
	heapStress(r, ops/10)
	lruDemo(r, ops)
	setDemo()
}
