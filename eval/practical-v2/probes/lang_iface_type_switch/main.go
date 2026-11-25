package main

import "fmt"

type Big struct{ n int }

func main() {
	for _, v := range []any{int64(1), &Big{2}, 1.5, "s", nil, []any{1}} {
		switch x := v.(type) {
		case int64, float64:
			fmt.Println("num", x)
		case *Big:
			fmt.Println("big", x.n)
		case nil:
			fmt.Println("nil")
		default:
			fmt.Printf("%T\n", x)
		}
	}
}
