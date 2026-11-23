package main

import "fmt"

type entry[K comparable, V any] struct {
	k K
	v V
}

func get[K comparable, V any](x any) V { return x.(*entry[K, V]).v }

func main() { fmt.Println(get[string, int](&entry[string, int]{"a", 1})) }
