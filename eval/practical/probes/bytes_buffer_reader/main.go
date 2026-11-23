package main

import (
	"bytes"
	"fmt"
	"io"
)

func main() {
	var b bytes.Buffer
	b.WriteString("hello")
	b.Truncate(3)
	r := bytes.NewReader(b.Bytes())
	p := make([]byte, 2)
	n, err := io.ReadFull(r, p)
	c, _ := r.ReadByte()
	fmt.Println(n, err, string(p), string(c), r.Len())
}
