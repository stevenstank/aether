// http checks net/http over Aether's network poller (D-057): a server and
// clients over loopback, keep-alive connections reused, concurrent
// requests, a request canceled by its context, client and server
// timeouts, httptest's server and recorder, TLS, HTTP/2, graceful shutdown, and a
// goroutine that keeps running while others wait on the network.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func newMux(release <-chan struct{}) *http.ServeMux {
	var mu sync.Mutex
	items := map[int]item{}
	next := 1
	mux := http.NewServeMux()
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		var it item
		if err := json.NewDecoder(r.Body).Decode(&it); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		mu.Lock()
		it.ID = next
		next++
		items[it.ID] = it
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(it)
	})
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id int
		fmt.Sscan(r.PathValue("id"), &id)
		mu.Lock()
		it, ok := items[id]
		mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(it)
	})
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
			io.WriteString(w, "released")
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			io.WriteString(w, "late")
		}
	})
	mux.HandleFunc("GET /big", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("0123456789", 100000)))
	})
	return mux
}

func main() {
	release := make(chan struct{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	var conns atomic.Int32
	srv := &http.Server{Handler: newMux(release), ConnState: func(c net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()

	// Keep-alive: sequential requests of one client share a connection.
	client := &http.Client{}
	for i := range 3 {
		resp, err := client.Post(base+"/items", "application/json", strings.NewReader(fmt.Sprintf(`{"name":"n%d"}`, i)))
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Print(resp.StatusCode, " ", resp.Header.Get("Content-Type"), " ", string(b))
	}
	resp, err := client.Get(base + "/items/2")
	if err != nil {
		panic(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Print(resp.Status, " ", string(b))
	resp, _ = client.Get(base + "/items/99")
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	fmt.Println(resp.Status, "connections:", conns.Load())

	// A goroutine keeps running while the requests below wait.
	var ticks atomic.Int32
	stopTicks := make(chan struct{})
	tickDone := make(chan struct{})
	go func() {
		defer close(tickDone)
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				ticks.Add(1)
			case <-stopTicks:
				return
			}
		}
	}()

	// Concurrent requests.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var names []string
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(base+"/items", "application/json", strings.NewReader(fmt.Sprintf(`{"name":"c%02d"}`, i)))
			if err != nil {
				panic(err)
			}
			var it item
			json.NewDecoder(resp.Body).Decode(&it)
			resp.Body.Close()
			mu.Lock()
			names = append(names, it.Name)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(names)
	fmt.Println(len(names), names[0], names[15])

	// A large body, read in parts.
	resp, err = client.Get(base + "/big")
	if err != nil {
		panic(err)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	fmt.Println("big:", n, err)

	// Cancellation by the request's context.
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/slow", nil)
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err = client.Do(req)
	fmt.Println("canceled:", errors.Is(err, context.Canceled), time.Since(start) < 4*time.Second)

	// The client's timeout.
	short := &http.Client{Timeout: 30 * time.Millisecond}
	_, err = short.Get(base + "/slow")
	var ue interface{ Timeout() bool }
	fmt.Println("timeout:", errors.As(err, &ue) && ue.Timeout(), strings.Contains(err.Error(), "Client.Timeout exceeded"))

	// A request that waits, then is released.
	got := make(chan string)
	go func() {
		resp, err := client.Get(base + "/slow")
		if err != nil {
			got <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got <- string(b)
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	fmt.Println("released:", <-got)
	close(stopTicks)
	<-tickDone
	fmt.Println("ticked while waiting:", ticks.Load() > 5)

	// The server's read timeout ends a connection that sends nothing.
	rt := &http.Server{Handler: newMux(nil), ReadTimeout: 30 * time.Millisecond}
	rln, _ := net.Listen("tcp", "127.0.0.1:0")
	go rt.Serve(rln)
	c, err := net.Dial("tcp", rln.Addr().String())
	if err != nil {
		panic(err)
	}
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	m, err := c.Read(make([]byte, 10))
	fmt.Println("read timeout:", m, err == io.EOF)
	c.Close()
	rt.Close()

	// httptest: a recorder, a server, and a TLS server with its client.
	rec := httptest.NewRecorder()
	newMux(nil).ServeHTTP(rec, httptest.NewRequest("GET", "/items/1", nil))
	fmt.Println("recorder:", rec.Code, strings.TrimSpace(rec.Body.String()))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s %s", r.Method, r.URL.Path, r.Header.Get("X-Test"))
	}))
	req, _ = http.NewRequest("PUT", ts.URL+"/p", nil)
	req.Header.Set("X-Test", "yes")
	resp, err = ts.Client().Do(req)
	if err != nil {
		panic(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("httptest:", string(b))
	ts.Close()
	tls := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "tls %v %s", r.TLS != nil, r.Proto)
	}))
	// The failed handshake below is logged with the time, and with what
	// the server read first: the client's alert, or its close.
	tls.Config.ErrorLog = log.New(io.Discard, "", 0)
	tls.StartTLS()
	resp, err = tls.Client().Get(tls.URL)
	if err != nil {
		panic(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("tls:", string(b), resp.TLS != nil && resp.TLS.HandshakeComplete)
	_, err = http.Get(tls.URL) // the test certificate is not trusted
	fmt.Println("untrusted:", err != nil && strings.Contains(err.Error(), "certificate"))
	tls.Close()

	// HTTP/2 over TLS, its streams on one connection.
	h2 := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s", r.Proto, r.URL.Path)
	}))
	h2.EnableHTTP2 = true
	h2.Config.ErrorLog = log.New(io.Discard, "", 0)
	h2.StartTLS()
	for i := range 3 {
		resp, err := h2.Client().Get(fmt.Sprintf("%s/s%d", h2.URL, i))
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println("h2:", resp.Proto, string(b))
	}
	h2.Close()

	// Graceful shutdown.
	client.CloseIdleConnections()
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	fmt.Println("shutdown:", srv.Shutdown(sctx), errors.Is(<-served, http.ErrServerClosed))
}
