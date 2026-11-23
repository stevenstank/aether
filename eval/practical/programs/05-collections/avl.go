package main

import "cmp"

// Tree is an ordered map implemented as an AVL tree.
type Tree[K cmp.Ordered, V any] struct {
	root *node[K, V]
	size int
}

type node[K cmp.Ordered, V any] struct {
	key         K
	val         V
	height      int
	left, right *node[K, V]
}

func height[K cmp.Ordered, V any](n *node[K, V]) int {
	if n == nil {
		return 0
	}
	return n.height
}

func (n *node[K, V]) fix() {
	n.height = 1 + max(height(n.left), height(n.right))
}

func (n *node[K, V]) balance() int { return height(n.left) - height(n.right) }

func rotateRight[K cmp.Ordered, V any](n *node[K, V]) *node[K, V] {
	l := n.left
	n.left = l.right
	l.right = n
	n.fix()
	l.fix()
	return l
}

func rotateLeft[K cmp.Ordered, V any](n *node[K, V]) *node[K, V] {
	r := n.right
	n.right = r.left
	r.left = n
	n.fix()
	r.fix()
	return r
}

func rebalance[K cmp.Ordered, V any](n *node[K, V]) *node[K, V] {
	n.fix()
	switch b := n.balance(); {
	case b > 1:
		if n.left.balance() < 0 {
			n.left = rotateLeft(n.left)
		}
		return rotateRight(n)
	case b < -1:
		if n.right.balance() > 0 {
			n.right = rotateRight(n.right)
		}
		return rotateLeft(n)
	}
	return n
}

// Put inserts or replaces the value for key. It reports whether key was new.
func (t *Tree[K, V]) Put(key K, val V) bool {
	added := false
	var put func(n *node[K, V]) *node[K, V]
	put = func(n *node[K, V]) *node[K, V] {
		if n == nil {
			added = true
			return &node[K, V]{key: key, val: val, height: 1}
		}
		switch c := cmp.Compare(key, n.key); {
		case c < 0:
			n.left = put(n.left)
		case c > 0:
			n.right = put(n.right)
		default:
			n.val = val
			return n
		}
		return rebalance(n)
	}
	t.root = put(t.root)
	if added {
		t.size++
	}
	return added
}

func (t *Tree[K, V]) Get(key K) (V, bool) {
	n := t.root
	for n != nil {
		switch c := cmp.Compare(key, n.key); {
		case c < 0:
			n = n.left
		case c > 0:
			n = n.right
		default:
			return n.val, true
		}
	}
	var zero V
	return zero, false
}

// Delete removes key and reports whether it was present.
func (t *Tree[K, V]) Delete(key K) bool {
	removed := false
	var del func(n *node[K, V], key K) *node[K, V]
	del = func(n *node[K, V], key K) *node[K, V] {
		if n == nil {
			return nil
		}
		switch c := cmp.Compare(key, n.key); {
		case c < 0:
			n.left = del(n.left, key)
		case c > 0:
			n.right = del(n.right, key)
		default:
			removed = true
			if n.left == nil {
				return n.right
			}
			if n.right == nil {
				return n.left
			}
			succ := n.right
			for succ.left != nil {
				succ = succ.left
			}
			n.key, n.val = succ.key, succ.val
			n.right = del(n.right, succ.key)
		}
		return rebalance(n)
	}
	t.root = del(t.root, key)
	if removed {
		t.size--
	}
	return removed
}

func (t *Tree[K, V]) Len() int { return t.size }

// Ascend calls f for each entry with lo <= key < hi in order, stopping
// early if f returns false.
func (t *Tree[K, V]) Ascend(lo, hi K, f func(K, V) bool) {
	var walk func(n *node[K, V]) bool
	walk = func(n *node[K, V]) bool {
		if n == nil {
			return true
		}
		if cmp.Less(lo, n.key) || lo == n.key {
			if !walk(n.left) {
				return false
			}
		}
		if (lo == n.key || cmp.Less(lo, n.key)) && cmp.Less(n.key, hi) {
			if !f(n.key, n.val) {
				return false
			}
		}
		if cmp.Less(n.key, hi) {
			return walk(n.right)
		}
		return true
	}
	walk(t.root)
}

// Height returns the height of the tree, for checking balance.
func (t *Tree[K, V]) Height() int { return height(t.root) }

// check verifies ordering and AVL invariants, returning the node count.
func (t *Tree[K, V]) check() int {
	var rec func(n *node[K, V], lo, hi *K) int
	rec = func(n *node[K, V], lo, hi *K) int {
		if n == nil {
			return 0
		}
		if lo != nil && n.key <= *lo || hi != nil && n.key >= *hi {
			panic("avl: order violated")
		}
		if b := n.balance(); b < -1 || b > 1 {
			panic("avl: balance violated")
		}
		if n.height != 1+max(height(n.left), height(n.right)) {
			panic("avl: stale height")
		}
		return 1 + rec(n.left, lo, &n.key) + rec(n.right, &n.key, hi)
	}
	return rec(t.root, nil, nil)
}
