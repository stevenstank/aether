package main

import (
	"container/list"
	"fmt"
)

func main() {
	l := list.New()
	l.PushBack(1)
	l.PushFront(0)
	for e := l.Front(); e != nil; e = e.Next() {
		fmt.Println(e.Value)
	}
}
