package main

import (
	"bytes"
	"fmt"
)

func main() {
	var buf bytes.Buffer
	buf.WriteString("abc")
	fmt.Println(buf.Len(), bytes.Count([]byte("a\na\n"), []byte("\n")), bytes.Equal([]byte("x"), []byte("x")))
	r := bytes.NewReader([]byte("q"))
	fmt.Println(r.Len())
}
