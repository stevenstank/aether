package main

// Heap is a binary min-heap ordered by less.
type Heap[T any] struct {
	items []T
	less  func(a, b T) bool
}

func NewHeap[T any](less func(a, b T) bool) *Heap[T] {
	return &Heap[T]{less: less}
}

func (h *Heap[T]) Len() int { return len(h.items) }

func (h *Heap[T]) Push(x T) {
	h.items = append(h.items, x)
	i := len(h.items) - 1
	for i > 0 {
		p := (i - 1) / 2
		if !h.less(h.items[i], h.items[p]) {
			break
		}
		h.items[i], h.items[p] = h.items[p], h.items[i]
		i = p
	}
}

func (h *Heap[T]) Peek() (T, bool) {
	if len(h.items) == 0 {
		var zero T
		return zero, false
	}
	return h.items[0], true
}

func (h *Heap[T]) Pop() (T, bool) {
	var zero T
	if len(h.items) == 0 {
		return zero, false
	}
	top := h.items[0]
	last := len(h.items) - 1
	h.items[0] = h.items[last]
	h.items[last] = zero
	h.items = h.items[:last]
	i := 0
	for {
		l, r, m := 2*i+1, 2*i+2, i
		if l < len(h.items) && h.less(h.items[l], h.items[m]) {
			m = l
		}
		if r < len(h.items) && h.less(h.items[r], h.items[m]) {
			m = r
		}
		if m == i {
			break
		}
		h.items[i], h.items[m] = h.items[m], h.items[i]
		i = m
	}
	return top, true
}
