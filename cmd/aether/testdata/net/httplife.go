// httplife checks net/http's lifecycle under a sustained load against gc
// (D-058): 2,400 keep-alive requests from eight concurrent clients over at
// most two connections each, in bounded memory; a streaming response
// canceled mid-body; the server's write timeout; a graceful shutdown that
// waits for the request in flight and refuses the next; and, once idle
// connections are closed, every descriptor and goroutine released.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func fds() int {
	es, _ := os.ReadDir("/proc/self/fd")
	return len(es)
}

func rss() int {
	b, _ := os.ReadFile("/proc/self/statm")
	var size, res int
	fmt.Sscan(string(b), &size, &res)
	return res * 4096
}

func main() {
	g0 := runtime.NumGoroutine()
	var conns atomic.Int32
	inflight := make(chan struct{})
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Write(b)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		for i := 0; ; i++ {
			if _, err := fmt.Fprintf(w, "chunk %d\n", i); err != nil {
				return
			}
			f.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
	})
	mux.HandleFunc("/hold", func(w http.ResponseWriter, r *http.Request) {
		close(inflight)
		<-release
		io.WriteString(w, "finished")
	})
	mux.HandleFunc("/slowwrite", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		io.WriteString(w, "late")
	})
	srv := &http.Server{Handler: mux, WriteTimeout: 50 * time.Millisecond, ConnState: func(c net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fd0 := fds() // the listener's and the poller's included
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()
	tr := &http.Transport{MaxIdleConnsPerHost: 8}
	client := &http.Client{Transport: tr}

	// Sustained keep-alive load: 8 clients, many requests each.
	const workers, each = 8, 150
	var wg sync.WaitGroup
	var bad atomic.Int32
	var mem0 int
	start := time.Now()
	for round := range 2 {
		for w := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range each {
					body := strings.Repeat(fmt.Sprint(w, i), 50)
					resp, err := client.Post(base+"/echo", "text/plain", strings.NewReader(body))
					if err != nil {
						bad.Add(1)
						continue
					}
					b, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					if string(b) != body {
						bad.Add(1)
					}
				}
			}()
		}
		wg.Wait()
		if round == 0 {
			runtime.GC()
			mem0 = rss()
		}
	}
	elapsed := time.Since(start)
	runtime.GC()
	fmt.Println("requests:", 2*workers*each, "failed:", bad.Load(), "connections reused:", conns.Load() <= 2*workers)
	fmt.Println("memory bounded:", rss() < 2*mem0+16<<20)

	// Cancel a streaming response mid-body.
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/stream", nil)
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	buf := make([]byte, 64)
	n, _ := io.ReadAtLeast(resp.Body, buf, 20)
	cancel()
	_, err = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	fmt.Println("stream read:", n >= 20, "canceled:", errors.Is(err, context.Canceled))

	// The server's write timeout cuts a slow handler's response.
	_, err = client.Get(base + "/slowwrite")
	fmt.Println("write timeout:", err != nil)

	// Graceful shutdown waits for the request in flight.
	got := make(chan string)
	go func() {
		resp, err := client.Get(base + "/hold")
		if err != nil {
			got <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got <- string(b)
	}()
	<-inflight
	shut := make(chan error)
	go func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		shut <- srv.Shutdown(sctx)
	}()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-shut:
		fmt.Println("shutdown returned early")
	default:
		fmt.Println("shutdown waits for the request")
	}
	close(release)
	fmt.Println("in flight:", <-got, "shutdown:", <-shut, errors.Is(<-served, http.ErrServerClosed))
	_, err = client.Get(base + "/echo")
	fmt.Println("refused after shutdown:", err != nil)

	// Everything is released.
	client.CloseIdleConnections()
	tr.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (fds() >= fd0 || runtime.NumGoroutine() > g0) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Println("descriptors released:", fds() < fd0, "goroutines ended:", runtime.NumGoroutine() <= g0)
	if len(os.Args) > 1 {
		fmt.Println(fds(), fd0, runtime.NumGoroutine(), g0, rss()>>10, mem0>>10, elapsed)
	}
}
