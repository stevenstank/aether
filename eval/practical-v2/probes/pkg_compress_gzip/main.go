package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

func main() {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Name = "data"
	zw.Write([]byte("hello hello hello"))
	zw.Close()
	zr, err := gzip.NewReader(&buf)
	if err != nil {
		panic(err)
	}
	b, _ := io.ReadAll(zr)
	fmt.Println(string(b))
}
