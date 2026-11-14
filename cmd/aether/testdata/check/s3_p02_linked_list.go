// aether:expect run

package main

// A recursive struct type through a pointer: a list built on the heap,
// walked, reversed in place, and searched; nil, pointer equality, and a
// switch on pointers.

type Node struct {
	Value int
	Next  *Node
}

type List struct {
	Head *Node
	Len  int
}

func which(p, a, b *Node) {
	switch p {
	case nil:
		println("nil")
	case a:
		println("a")
	case b:
		println("b")
	default:
		println("other")
	}
}

func push(l *List, v int) {
	l.Head = &Node{Value: v, Next: l.Head}
	l.Len++
}

func sum(n *Node) int {
	s := 0
	for ; n != nil; n = n.Next {
		s += n.Value
	}
	return s
}

func reverse(l *List) {
	var prev *Node
	cur := l.Head
	for cur != nil {
		next := cur.Next
		cur.Next = prev
		prev, cur = cur, next
	}
	l.Head = prev
}

func find(l List, v int) *Node {
	for n := l.Head; n != nil; n = n.Next {
		if n.Value == v {
			return n
		}
	}
	return nil
}

func show(l List) {
	for n := l.Head; n != nil; n = n.Next {
		print(n.Value, " ")
	}
	println(l.Len)
}

func main() {
	var l List
	for i := 1; i <= 5; i++ {
		push(&l, i*i)
	}
	show(l)
	println(sum(l.Head))
	reverse(&l)
	show(l)
	n := find(l, 9)
	n.Value = 90
	show(l)
	println(find(l, 7) == nil, find(l, 90) == n, l.Head.Next.Next == n)
	which(n, l.Head, n)
	which(l.Head, l.Head, n)
	which(nil, l.Head, n)
	which(&Node{9, nil}, l.Head, n)
}
