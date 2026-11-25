package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	sc := bufio.NewScanner(strings.NewReader("a b\nc"))
	sc.Buffer(make([]byte, 16), 1024)
	for sc.Scan() {
		fmt.Println(sc.Text())
	}
	w := bufio.NewWriterSize(os.Stdout, 64)
	w.WriteString("hi\n")
	w.Flush()
	r := bufio.NewReader(strings.NewReader("xy"))
	b, _ := r.ReadByte()
	fmt.Println(b)
}
