package main

import (
	"container/heap"
	"fmt"
)

type h []int

func (x h) Len() int           { return len(x) }
func (x h) Less(i, j int) bool { return x[i] < x[j] }
func (x h) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x *h) Push(v any)        { *x = append(*x, v.(int)) }
func (x *h) Pop() any {
	old := *x
	v := old[len(old)-1]
	*x = old[:len(old)-1]
	return v
}

func main() {
	q := &h{5, 1, 3}
	heap.Init(q)
	heap.Push(q, 0)
	fmt.Println(heap.Pop(q), heap.Pop(q))
}
