package main

import (
	"fmt"
	"net/url"
)

func main() {
	b, _ := url.Parse("https://a.example/x/y")
	r, _ := url.Parse("../z?ref=1#f")
	u := b.ResolveReference(r)
	q := u.Query()
	q.Del("ref")
	fmt.Println(u.Host, u.Path, q.Encode())
}
