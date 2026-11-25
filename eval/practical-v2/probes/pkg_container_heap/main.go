package main

import (
	"container/heap"
	"fmt"
)

type ih []int

func (h ih) Len() int           { return len(h) }
func (h ih) Less(i, j int) bool { return h[i] < h[j] }
func (h ih) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *ih) Push(x any)        { *h = append(*h, x.(int)) }
func (h *ih) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func main() {
	h := &ih{5, 2, 8}
	heap.Init(h)
	heap.Push(h, 1)
	fmt.Println(heap.Pop(h), heap.Pop(h))
}
