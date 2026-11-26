// tcp checks package net over Aether's network poller (D-056): TCP over
// loopback with concurrent clients, reads and writes that wait (a
// transfer larger than the socket buffers), read deadlines, a listener
// closed while Accept waits, a refused dial, and UDP.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

func echo(ln net.Listener, accepted chan<- error) {
	for {
		c, err := ln.Accept()
		if err != nil {
			accepted <- err
			return
		}
		go func() {
			defer c.Close()
			r := bufio.NewReader(c)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				fmt.Fprintf(c, "echo %s", strings.ToUpper(line))
			}
		}()
	}
}

func clients(addr string) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var lines []string
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := net.Dial("tcp", addr)
			if err != nil {
				panic(err)
			}
			defer c.Close()
			r := bufio.NewReader(c)
			for j := range 4 {
				fmt.Fprintf(c, "client %d line %d\n", i, j)
				s, err := r.ReadString('\n')
				if err != nil {
					panic(err)
				}
				mu.Lock()
				lines = append(lines, strings.TrimSpace(s))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Strings(lines)
	fmt.Println(len(lines), lines[0], lines[len(lines)-1])
}

// bulk sends more than the socket buffers hold before the reader reads,
// so that the writer waits for the poller.
func bulk() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	const n = 8 << 20
	sum := make(chan int)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		defer c.Close()
		time.Sleep(20 * time.Millisecond)
		total, s := 0, 0
		buf := make([]byte, 64<<10)
		for {
			m, err := c.Read(buf)
			for _, b := range buf[:m] {
				s += int(b)
			}
			total += m
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
		}
		sum <- total
		sum <- s
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		panic(err)
	}
	data := make([]byte, n)
	want := 0
	for i := range data {
		data[i] = byte(i * 7)
		want += int(data[i])
	}
	m, err := c.Write(data)
	fmt.Println("wrote", m, err)
	c.Close()
	fmt.Println("read", <-sum, <-sum == want)
}

func deadlines() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	hold := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		hold <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		panic(err)
	}
	defer c.Close()
	start := time.Now()
	c.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	buf := make([]byte, 10)
	_, err = c.Read(buf)
	elapsed := time.Since(start)
	var ne net.Error
	fmt.Println("timeout:", errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout(),
		elapsed >= 30*time.Millisecond, elapsed < 2*time.Second)
	// A deadline already passed fails at once; clearing it lets the read
	// wait for data.
	c.SetReadDeadline(time.Now().Add(-time.Second))
	_, err = c.Read(buf)
	fmt.Println("passed:", errors.Is(err, os.ErrDeadlineExceeded))
	c.SetReadDeadline(time.Time{})
	peer := <-hold
	defer peer.Close()
	go func() {
		time.Sleep(10 * time.Millisecond)
		peer.Write([]byte("late"))
	}()
	m, err := c.Read(buf)
	fmt.Println("cleared:", string(buf[:m]), err)
	// A deadline set while a read waits ends it.
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.SetReadDeadline(time.Now())
	}()
	_, err = c.Read(buf)
	fmt.Println("set while waiting:", errors.Is(err, os.ErrDeadlineExceeded))
}

func udp() {
	a, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer a.Close()
	b, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer b.Close()
	got := make(chan string)
	go func() {
		buf := make([]byte, 100)
		n, from, err := b.ReadFrom(buf)
		got <- fmt.Sprint(string(buf[:n]), " ", from.String() == a.LocalAddr().String(), " ", err)
	}()
	time.Sleep(5 * time.Millisecond)
	if _, err := a.WriteTo([]byte("datagram"), b.LocalAddr()); err != nil {
		panic(err)
	}
	fmt.Println("udp:", <-got)
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Println(ln.Addr().Network(), strings.HasPrefix(ln.Addr().String(), "127.0.0.1:"))
	accepted := make(chan error)
	go echo(ln, accepted)
	clients(ln.Addr().String())
	addr := ln.Addr().String()
	ln.Close()
	err = <-accepted
	fmt.Println("accept after close:", errors.Is(err, net.ErrClosed))
	_, err = net.Dial("tcp", addr)
	var oe *net.OpError
	fmt.Println("refused:", errors.As(err, &oe), oe.Op, strings.Contains(err.Error(), "connection refused"))

	bulk()
	deadlines()
	udp()

	host, port, err := net.SplitHostPort("[::1]:8080")
	fmt.Println(host, port, err)
	ta, err := net.ResolveTCPAddr("tcp", "127.0.0.1:80")
	fmt.Println(ta, err)
	ip := net.ParseIP("192.168.1.10")
	_, cidr, _ := net.ParseCIDR("192.168.0.0/16")
	fmt.Println(cidr.Contains(ip), ip.To4() != nil, net.IPv4(10, 0, 0, 1))
}
