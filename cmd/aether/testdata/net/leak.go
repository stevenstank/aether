// leak checks that a connection nobody closes is closed once it is
// collected, as gc's finalizer closes it (D-057): the server then reads
// end of file. Its client conn is made and dropped in a frame that has
// returned, whose stack is overwritten, so that the conservative
// collector (D-046) finds no stale pointer to it.
package main

import (
	"fmt"
	"io"
	"net"
	"runtime"
	"time"
)

//go:noinline
func dialAndDrop(addr string) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		panic(err)
	}
	c.Write([]byte("hi"))
}

//go:noinline
func clobber(n int) int {
	var buf [256]int
	for i := range buf {
		buf[i] = n
	}
	if n > 0 {
		return clobber(n-1) + buf[n%256]
	}
	return buf[0]
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	result := make(chan string)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		b, err := io.ReadAll(c)
		result <- fmt.Sprint(string(b), " ", err)
	}()
	dialAndDrop(ln.Addr().String())
	clobber(50)
	deadline := time.After(10 * time.Second)
	for {
		runtime.GC()
		select {
		case s := <-result:
			fmt.Println("server read:", s)
			return
		case <-deadline:
			fmt.Println("never closed")
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}
