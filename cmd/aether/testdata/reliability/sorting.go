// sort.Slice, sort.SliceStable, and sort.SliceIsSorted over elements of
// many sizes and shapes: bytes, ints of each width, strings, pointers,
// interfaces, arrays, structs of odd sizes with pointers inside, and an
// empty struct. The less functions allocate, so the collector runs while
// an element is held only in the swapper's scratch space. Misuse panics
// with reflect's messages, and is recovered.
package main

import (
	"fmt"
	"sort"
	"strings"
)

type Rec struct {
	Name  string
	Score int16
	Flag  bool
	Next  *Rec
	Notes []string
}

type Odd struct {
	A byte
	B [3]byte
}

type Names []string

var sink [][]byte

// churn allocates, so the collector runs during a sort.
func churn(n int) {
	for i := 0; i < n; i++ {
		sink = append(sink, make([]byte, 64+i))
	}
	if len(sink) > 200 {
		sink = sink[:0]
	}
}

func tryCall(what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(what, "panic:", r)
		}
	}()
	f()
	fmt.Println(what, "ok")
}

func main() {
	bs := []byte("the quick brown fox")
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	fmt.Printf("%q\n", bs)

	i16 := []int16{300, -2, 7, -32768, 32767, 0}
	sort.Slice(i16, func(i, j int) bool { return i16[i] > i16[j] })
	u32 := []uint32{4e9, 1, 70000, 3}
	sort.Slice(u32, func(i, j int) bool { return u32[i] < u32[j] })
	f64 := []float64{2.5, -1, 1e10, 0.125}
	sort.Slice(f64, func(i, j int) bool { return f64[i] < f64[j] })
	fmt.Println(i16, u32, f64)

	words := Names(strings.Fields("pear fig apple kiwi banana cherry date grape"))
	sort.Slice(words, func(i, j int) bool {
		churn(2)
		return len(words[i]) < len(words[j]) || len(words[i]) == len(words[j]) && words[i] < words[j]
	})
	fmt.Println(words, sort.SliceIsSorted(words, func(i, j int) bool { return len(words[i]) < len(words[j]) }))

	var recs []*Rec
	var prev *Rec
	for i := 0; i < 40; i++ {
		r := &Rec{Name: fmt.Sprintf("r%02d", (i*17)%40), Score: int16((i * 37) % 11), Flag: i%3 == 0, Next: prev}
		r.Notes = []string{strings.Repeat("n", i%4)}
		recs = append(recs, r)
		prev = r
	}
	sort.SliceStable(recs, func(i, j int) bool { churn(1); return recs[i].Score < recs[j].Score })
	var b strings.Builder
	for _, r := range recs[:12] {
		fmt.Fprintf(&b, "%s/%d ", r.Name, r.Score)
	}
	fmt.Println(b.String())

	vals := make([]Rec, len(recs))
	for i, r := range recs {
		vals[i] = *r
		vals[i].Notes = append([]string{fmt.Sprint("copy", i)}, r.Notes...)
	}
	recs = nil
	sort.Slice(vals, func(i, j int) bool {
		churn(3)
		if vals[i].Flag != vals[j].Flag {
			return vals[i].Flag
		}
		return vals[i].Name > vals[j].Name
	})
	chain := 0
	for _, v := range vals {
		for p := v.Next; p != nil; p = p.Next {
			chain++
		}
	}
	fmt.Println(vals[0].Name, vals[0].Notes, vals[len(vals)-1].Name, vals[len(vals)-1].Notes, "chain", chain)

	anys := []any{3, "b", 1.5, nil, []int{1}, 'x', "a"}
	key := func(v any) string { return fmt.Sprintf("%T:%v", v, v) }
	sort.Slice(anys, func(i, j int) bool { return key(anys[i]) < key(anys[j]) })
	fmt.Println(anys)

	arrs := [][2]string{{"b", "1"}, {"a", "2"}, {"b", "0"}, {"a", "1"}}
	sort.SliceStable(arrs, func(i, j int) bool { return arrs[i][0] < arrs[j][0] })
	odds := []Odd{{3, [3]byte{1, 2, 3}}, {1, [3]byte{9}}, {2, [3]byte{}}, {1, [3]byte{4}}}
	sort.SliceStable(odds, func(i, j int) bool { return odds[i].A < odds[j].A })
	empties := make([]struct{}, 5)
	sort.Slice(empties, func(i, j int) bool { return i < j })
	fmt.Println(arrs, odds, len(empties))

	big := make([]int, 500)
	for i := range big {
		big[i] = (i * 7919) % 503
	}
	sort.Slice(big, func(i, j int) bool { return big[i] < big[j] })
	fmt.Println(big[:5], big[495:], sort.SliceIsSorted(big, func(i, j int) bool { return big[i] < big[j] }))

	tryCall("empty", func() { sort.Slice([]int{}, func(i, j int) bool { return false }) })
	tryCall("nil slice", func() { sort.Slice([]string(nil), func(i, j int) bool { return false }) })
	tryCall("one", func() { sort.SliceStable([]Rec{{}}, func(i, j int) bool { return false }) })
	tryCall("int", func() { sort.Slice(3, func(i, j int) bool { return false }) })
	tryCall("array", func() { sort.Slice([2]int{2, 1}, func(i, j int) bool { return false }) })
	tryCall("nil", func() { sort.SliceStable(nil, func(i, j int) bool { return false }) })
	tryCall("pointer", func() { sort.SliceIsSorted(&bs, func(i, j int) bool { return false }) })
	tryCall("less panics", func() {
		sort.Slice(f64, func(i, j int) bool {
			if f64[i] > 1e9 {
				panic(fmt.Sprint("big ", f64[i]))
			}
			return f64[i] < f64[j]
		})
	})
}
