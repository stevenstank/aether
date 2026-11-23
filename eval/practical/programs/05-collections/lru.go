package main

import "container/list"

// LRU is a fixed-capacity cache evicting the least recently used entry.
type LRU[K comparable, V any] struct {
	cap     int
	ll      *list.List
	items   map[K]*list.Element
	OnEvict func(K, V)

	Hits, Misses int
}

type lruEntry[K comparable, V any] struct {
	key K
	val V
}

func NewLRU[K comparable, V any](capacity int) *LRU[K, V] {
	if capacity <= 0 {
		panic("lru: capacity must be positive")
	}
	return &LRU[K, V]{cap: capacity, ll: list.New(), items: make(map[K]*list.Element)}
}

func (c *LRU[K, V]) Get(k K) (V, bool) {
	if e, ok := c.items[k]; ok {
		c.ll.MoveToFront(e)
		c.Hits++
		return e.Value.(*lruEntry[K, V]).val, true
	}
	c.Misses++
	var zero V
	return zero, false
}

func (c *LRU[K, V]) Put(k K, v V) {
	if e, ok := c.items[k]; ok {
		e.Value.(*lruEntry[K, V]).val = v
		c.ll.MoveToFront(e)
		return
	}
	c.items[k] = c.ll.PushFront(&lruEntry[K, V]{k, v})
	if c.ll.Len() > c.cap {
		old := c.ll.Back()
		ent := c.ll.Remove(old).(*lruEntry[K, V])
		delete(c.items, ent.key)
		if c.OnEvict != nil {
			c.OnEvict(ent.key, ent.val)
		}
	}
}

func (c *LRU[K, V]) Len() int { return c.ll.Len() }

// Keys returns keys from most to least recently used.
func (c *LRU[K, V]) Keys() []K {
	keys := make([]K, 0, c.ll.Len())
	for e := c.ll.Front(); e != nil; e = e.Next() {
		keys = append(keys, e.Value.(*lruEntry[K, V]).key)
	}
	return keys
}
