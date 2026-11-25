package main

import (
	"fmt"
	"io"
	"os"
)

type rec struct {
	io.Writer
	n int
}

func (r *rec) Write(b []byte) (int, error) {
	r.n += len(b)
	return r.Writer.Write(b)
}

func main() {
	r := &rec{Writer: os.Stdout}
	r.Write([]byte("hi\n"))
	fmt.Println(r.n)
}
