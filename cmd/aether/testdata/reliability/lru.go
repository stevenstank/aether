// A generic LRU cache over a doubly linked list and a map, driven hard
// enough to make the collector run many times: evicted entries hold
// slices, strings, and closures that become garbage.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

type entry[K comparable, V any] struct {
	key        K
	val        V
	prev, next *entry[K, V]
}

type LRU[K comparable, V any] struct {
	cap        int
	m          map[K]*entry[K, V]
	head, tail *entry[K, V] // sentinels
	onEvict    func(K, V)
	hits, miss int
}

func NewLRU[K comparable, V any](n int, onEvict func(K, V)) *LRU[K, V] {
	c := &LRU[K, V]{cap: n, m: make(map[K]*entry[K, V]), onEvict: onEvict}
	c.head, c.tail = &entry[K, V]{}, &entry[K, V]{}
	c.head.next, c.tail.prev = c.tail, c.head
	return c
}

func (c *LRU[K, V]) unlink(e *entry[K, V]) {
	e.prev.next, e.next.prev = e.next, e.prev
	e.prev, e.next = nil, nil
}

func (c *LRU[K, V]) front(e *entry[K, V]) {
	e.next, e.prev = c.head.next, c.head
	c.head.next.prev = e
	c.head.next = e
}

func (c *LRU[K, V]) Get(k K) (V, bool) {
	if e, ok := c.m[k]; ok {
		c.hits++
		c.unlink(e)
		c.front(e)
		return e.val, true
	}
	c.miss++
	var zero V
	return zero, false
}

func (c *LRU[K, V]) Put(k K, v V) {
	if e, ok := c.m[k]; ok {
		e.val = v
		c.unlink(e)
		c.front(e)
		return
	}
	if len(c.m) == c.cap {
		old := c.tail.prev
		c.unlink(old)
		delete(c.m, old.key)
		if c.onEvict != nil {
			c.onEvict(old.key, old.val)
		}
	}
	e := &entry[K, V]{key: k, val: v}
	c.m[k] = e
	c.front(e)
}

func (c *LRU[K, V]) Keys() []K {
	var ks []K
	for e := c.head.next; e != c.tail; e = e.next {
		ks = append(ks, e.key)
	}
	return ks
}

type blob struct {
	name  string
	data  []int
	check func() int
}

func makeBlob(i int) *blob {
	data := make([]int, 16+i%48)
	for j := range data {
		data[j] = i*31 + j
	}
	b := &blob{name: "blob-" + strconv.Itoa(i), data: data}
	b.check = func() int {
		s := 0
		for _, x := range b.data {
			s += x
		}
		return s + len(b.name)
	}
	return b
}

type key struct {
	a int
	b string
}

func main() {
	evicted, evictSum := 0, 0
	c := NewLRU[int, *blob](64, func(k int, b *blob) {
		evicted++
		evictSum += b.check() % 1000
	})
	x := uint64(12345)
	for i := 0; i < 40000; i++ {
		x = x*6364136223846793005 + 1442695040888963407
		k := int(x>>33) % 200
		if b, ok := c.Get(k); ok {
			if b.name != "blob-"+strconv.Itoa(k) || b.data[0] != k*31 {
				fmt.Println("corrupt entry", k, b.name)
				return
			}
			continue
		}
		c.Put(k, makeBlob(k))
	}
	fmt.Println("hits", c.hits, "misses", c.miss, "evicted", evicted, "sum", evictSum)
	ks := c.Keys()
	fmt.Println("len", len(ks), "front", ks[:8])
	total := 0
	for _, k := range ks {
		b, _ := c.Get(k)
		total += b.check()
	}
	fmt.Println("check total", total)

	sc := NewLRU[key, []string](10, nil)
	for i := 0; i < 500; i++ {
		k := key{i % 13, strings.Repeat("k", i%3)}
		v, ok := sc.Get(k)
		v = append(v, fmt.Sprint(i))
		if len(v) > 4 {
			v = v[len(v)-4:]
		}
		sc.Put(k, v)
		_ = ok
	}
	for _, k := range sc.Keys() {
		v, _ := sc.Get(k)
		fmt.Printf("%d/%q=%v ", k.a, k.b, v)
	}
	fmt.Println()
}
