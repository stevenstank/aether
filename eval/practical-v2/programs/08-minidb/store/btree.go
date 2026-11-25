// Package store implements tables, B-tree indexes, and an undo log for
// the minidb engine.
package store

// BTree is an ordered map from K to V (CLRS-style B-tree with minimum
// degree t). Keys are unique under cmp.
type BTree[K, V any] struct {
	root *bnode[K, V]
	cmp  func(a, b K) int
	t    int
	size int
}

type bnode[K, V any] struct {
	keys     []K
	vals     []V
	children []*bnode[K, V]
}

func (n *bnode[K, V]) leaf() bool { return len(n.children) == 0 }

func NewBTree[K, V any](degree int, cmp func(a, b K) int) *BTree[K, V] {
	if degree < 2 {
		degree = 2
	}
	return &BTree[K, V]{root: &bnode[K, V]{}, cmp: cmp, t: degree}
}

func (b *BTree[K, V]) Len() int { return b.size }

// find returns the first index i with keys[i] >= k and whether it is equal.
func (b *BTree[K, V]) find(n *bnode[K, V], k K) (int, bool) {
	lo, hi := 0, len(n.keys)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if b.cmp(n.keys[m], k) < 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo, lo < len(n.keys) && b.cmp(n.keys[lo], k) == 0
}

func (b *BTree[K, V]) Get(k K) (V, bool) {
	n := b.root
	for {
		i, eq := b.find(n, k)
		if eq {
			return n.vals[i], true
		}
		if n.leaf() {
			var zero V
			return zero, false
		}
		n = n.children[i]
	}
}

func insertAt[T any](s []T, i int, v T) []T {
	var zero T
	s = append(s, zero)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

func removeAt[T any](s []T, i int) []T {
	copy(s[i:], s[i+1:])
	var zero T
	s[len(s)-1] = zero
	return s[:len(s)-1]
}

func (b *BTree[K, V]) splitChild(x *bnode[K, V], i int) {
	t := b.t
	y := x.children[i]
	z := &bnode[K, V]{}
	z.keys = append(z.keys, y.keys[t:]...)
	z.vals = append(z.vals, y.vals[t:]...)
	if !y.leaf() {
		z.children = append(z.children, y.children[t:]...)
		y.children = y.children[:t]
	}
	midK, midV := y.keys[t-1], y.vals[t-1]
	y.keys, y.vals = y.keys[:t-1], y.vals[:t-1]
	x.keys = insertAt(x.keys, i, midK)
	x.vals = insertAt(x.vals, i, midV)
	x.children = insertAt(x.children, i+1, z)
}

// Put inserts or replaces; it reports whether the key was new.
func (b *BTree[K, V]) Put(k K, v V) bool {
	r := b.root
	if len(r.keys) == 2*b.t-1 {
		s := &bnode[K, V]{children: []*bnode[K, V]{r}}
		b.root = s
		b.splitChild(s, 0)
	}
	n := b.root
	for {
		i, eq := b.find(n, k)
		if eq {
			n.vals[i] = v
			return false
		}
		if n.leaf() {
			n.keys = insertAt(n.keys, i, k)
			n.vals = insertAt(n.vals, i, v)
			b.size++
			return true
		}
		if len(n.children[i].keys) == 2*b.t-1 {
			b.splitChild(n, i)
			switch c := b.cmp(k, n.keys[i]); {
			case c == 0:
				n.vals[i] = v
				return false
			case c > 0:
				i++
			}
		}
		n = n.children[i]
	}
}

// Delete removes k and reports whether it was present.
func (b *BTree[K, V]) Delete(k K) bool {
	ok := b.delete(b.root, k)
	if len(b.root.keys) == 0 && !b.root.leaf() {
		b.root = b.root.children[0]
	}
	if ok {
		b.size--
	}
	return ok
}

func (b *BTree[K, V]) delete(n *bnode[K, V], k K) bool {
	t := b.t
	i, eq := b.find(n, k)
	if n.leaf() {
		if !eq {
			return false
		}
		n.keys = removeAt(n.keys, i)
		n.vals = removeAt(n.vals, i)
		return true
	}
	if eq {
		left, right := n.children[i], n.children[i+1]
		switch {
		case len(left.keys) >= t:
			pk, pv := b.max(left)
			n.keys[i], n.vals[i] = pk, pv
			return b.delete(left, pk)
		case len(right.keys) >= t:
			sk, sv := b.min(right)
			n.keys[i], n.vals[i] = sk, sv
			return b.delete(right, sk)
		default:
			b.merge(n, i)
			return b.delete(left, k)
		}
	}
	child := n.children[i]
	if len(child.keys) == t-1 {
		switch {
		case i > 0 && len(n.children[i-1].keys) >= t:
			sib := n.children[i-1]
			child.keys = insertAt(child.keys, 0, n.keys[i-1])
			child.vals = insertAt(child.vals, 0, n.vals[i-1])
			last := len(sib.keys) - 1
			n.keys[i-1], n.vals[i-1] = sib.keys[last], sib.vals[last]
			sib.keys, sib.vals = sib.keys[:last], sib.vals[:last]
			if !sib.leaf() {
				child.children = insertAt(child.children, 0, sib.children[len(sib.children)-1])
				sib.children = sib.children[:len(sib.children)-1]
			}
		case i < len(n.children)-1 && len(n.children[i+1].keys) >= t:
			sib := n.children[i+1]
			child.keys = append(child.keys, n.keys[i])
			child.vals = append(child.vals, n.vals[i])
			n.keys[i], n.vals[i] = sib.keys[0], sib.vals[0]
			sib.keys, sib.vals = removeAt(sib.keys, 0), removeAt(sib.vals, 0)
			if !sib.leaf() {
				child.children = append(child.children, sib.children[0])
				sib.children = removeAt(sib.children, 0)
			}
		case i < len(n.children)-1:
			b.merge(n, i)
		default:
			b.merge(n, i-1)
			child = n.children[i-1]
		}
	}
	return b.delete(child, k)
}

// merge folds keys[i] and children[i+1] into children[i].
func (b *BTree[K, V]) merge(n *bnode[K, V], i int) {
	left, right := n.children[i], n.children[i+1]
	left.keys = append(append(left.keys, n.keys[i]), right.keys...)
	left.vals = append(append(left.vals, n.vals[i]), right.vals...)
	left.children = append(left.children, right.children...)
	n.keys = removeAt(n.keys, i)
	n.vals = removeAt(n.vals, i)
	n.children = removeAt(n.children, i+1)
}

func (b *BTree[K, V]) max(n *bnode[K, V]) (K, V) {
	for !n.leaf() {
		n = n.children[len(n.children)-1]
	}
	return n.keys[len(n.keys)-1], n.vals[len(n.vals)-1]
}

func (b *BTree[K, V]) min(n *bnode[K, V]) (K, V) {
	for !n.leaf() {
		n = n.children[0]
	}
	return n.keys[0], n.vals[0]
}

// Ascend calls fn for keys >= from (or all keys when from is nil) in
// order until fn returns false.
func (b *BTree[K, V]) Ascend(from *K, fn func(K, V) bool) {
	b.ascend(b.root, from, fn)
}

func (b *BTree[K, V]) ascend(n *bnode[K, V], from *K, fn func(K, V) bool) bool {
	i := 0
	if from != nil {
		i, _ = b.find(n, *from)
	}
	for ; i < len(n.keys); i++ {
		if !n.leaf() && !b.ascend(n.children[i], from, fn) {
			return false
		}
		if !fn(n.keys[i], n.vals[i]) {
			return false
		}
		from = nil
	}
	if !n.leaf() {
		return b.ascend(n.children[len(n.keys)], from, fn)
	}
	return true
}

// All returns an iterator over every entry in key order.
func (b *BTree[K, V]) All() func(yield func(K, V) bool) {
	return func(yield func(K, V) bool) { b.Ascend(nil, yield) }
}

// Height returns the number of levels, for diagnostics.
func (b *BTree[K, V]) Height() int {
	h := 1
	for n := b.root; !n.leaf(); n = n.children[0] {
		h++
	}
	return h
}

// Check validates ordering and node fill; it returns a description of
// the first problem found, or "".
func (b *BTree[K, V]) Check() string {
	count := 0
	var prev *K
	var depth = -1
	var walk func(n *bnode[K, V], d int, isRoot bool) string
	walk = func(n *bnode[K, V], d int, isRoot bool) string {
		if !isRoot && (len(n.keys) < b.t-1 || len(n.keys) > 2*b.t-1) {
			return "node fill out of range"
		}
		if !n.leaf() && len(n.children) != len(n.keys)+1 {
			return "child count mismatch"
		}
		for i := range n.keys {
			if !n.leaf() {
				if p := walk(n.children[i], d+1, false); p != "" {
					return p
				}
			}
			if prev != nil && b.cmp(*prev, n.keys[i]) >= 0 {
				return "keys out of order"
			}
			k := n.keys[i]
			prev = &k
			count++
		}
		if !n.leaf() {
			return walk(n.children[len(n.keys)], d+1, false)
		}
		if depth == -1 {
			depth = d
		} else if depth != d {
			return "leaves at different depths"
		}
		return ""
	}
	if p := walk(b.root, 0, true); p != "" {
		return p
	}
	if count != b.size {
		return "size mismatch"
	}
	return ""
}
