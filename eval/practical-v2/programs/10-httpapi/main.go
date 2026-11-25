// Command bookmarks is a JSON HTTP API for per-user bookmarks.
//
//	bookmarks -script FILE        replay requests in-process (httptest)
//	bookmarks -selftest [-clients N -requests M]
//	                              serve on a loopback port and drive it with
//	                              concurrent HTTP clients
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

func fakeClock() func() time.Time {
	var mu sync.Mutex
	t := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t = t.Add(90 * time.Second)
		return t
	}
}

var tokens = map[string]string{"alice-token": "alice", "bob-token": "bob"}

type scriptReq struct {
	line    int
	method  string
	target  string
	headers [][2]string
	body    string
}

func parseScript(r io.Reader) ([]scriptReq, error) {
	sc := bufio.NewScanner(r)
	var reqs []scriptReq
	var cur *scriptReq
	state := 0 // 0: want request line, 1: headers, 2: body
	var body []string
	n := 0
	flush := func() {
		if cur != nil {
			cur.body = strings.TrimRight(strings.Join(body, "\n"), "\n")
			reqs = append(reqs, *cur)
		}
		cur, body, state = nil, nil, 0
	}
	for sc.Scan() {
		n++
		line := sc.Text()
		switch {
		case line == "---":
			flush()
		case state == 0:
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Fields(line)
			if len(f) != 2 {
				return nil, fmt.Errorf("line %d: want METHOD TARGET", n)
			}
			cur = &scriptReq{line: n, method: f[0], target: f[1]}
			state = 1
		case state == 1:
			if line == "" {
				state = 2
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("line %d: bad header %q", n, line)
			}
			cur.headers = append(cur.headers, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
		default:
			body = append(body, line)
		}
	}
	flush()
	return reqs, sc.Err()
}

func runScript(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	reqs, err := parseScript(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	srv := NewServer(NewStore(fakeClock()), tokens, os.Stderr)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, sr := range reqs {
		req := httptest.NewRequest(sr.method, sr.target, strings.NewReader(sr.body))
		for _, h := range sr.headers {
			req.Header.Add(h[0], h[1])
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		res := rec.Result()
		fmt.Fprintf(out, "> %s %s\n< %s\n", sr.method, sr.target, res.Status)
		keys := make([]string, 0, len(res.Header))
		for k := range res.Header {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Fprintf(out, "< %s: %s\n", k, strings.Join(res.Header[k], ", "))
		}
		b, _ := io.ReadAll(res.Body)
		out.Write(b)
		out.WriteString("\n")
	}
	return nil
}

type tally struct {
	mu     sync.Mutex
	status map[int]int
	errs   []string
}

func (t *tally) add(code int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.errs = append(t.errs, err.Error())
		return
	}
	t.status[code]++
}

func selftest(clients, requests int) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	tok := map[string]string{}
	for i := 0; i < clients; i++ {
		tok[fmt.Sprintf("token-%d", i)] = fmt.Sprintf("user%d", i)
	}
	store := NewStore(fakeClock())
	hs := &http.Server{Handler: NewServer(store, tok, io.Discard), ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- hs.Serve(ln) }()
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 10 * time.Second}

	do := func(method, path, token string, body any) (int, []byte, error) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, base+path, rd)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		return resp.StatusCode, data, err
	}

	t := &tally{status: map[int]int{}}
	var wg sync.WaitGroup
	results := make([]string, clients)
	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := fmt.Sprintf("token-%d", c)
			var ids []int
			for i := 0; i < requests; i++ {
				code, data, err := do("POST", "/api/bookmarks", token, map[string]any{
					"url": fmt.Sprintf("https://example.com/%d/%d", c, i), "title": fmt.Sprintf("Item %d", i),
					"tags": []string{fmt.Sprintf("t%d", i%3), "load"}})
				t.add(code, err)
				var b Bookmark
				if err == nil && json.Unmarshal(data, &b) == nil {
					ids = append(ids, b.ID)
				}
			}
			// A duplicate and an invalid request per client.
			code, _, err := do("POST", "/api/bookmarks", token, map[string]any{"url": fmt.Sprintf("https://example.com/%d/0", c), "title": "dup"})
			t.add(code, err)
			code, _, err = do("POST", "/api/bookmarks", token, map[string]any{"url": "ftp://x", "title": ""})
			t.add(code, err)
			for k, id := range ids {
				if k%2 == 0 {
					code, _, err := do("DELETE", fmt.Sprintf("/api/bookmarks/%d", id), token, nil)
					t.add(code, err)
				}
			}
			code, data, err := do("GET", "/api/bookmarks?tag=load&limit=100", token, nil)
			t.add(code, err)
			var page []Bookmark
			json.Unmarshal(data, &page)
			owned := 0
			for _, b := range page {
				if b.Owner == fmt.Sprintf("user%d", c) {
					owned++
				}
			}
			results[c] = fmt.Sprintf("client %d: created %d, listed %d (all owned: %v)", c, len(ids), len(page), owned == len(page))
		}()
	}
	wg.Wait()
	// Unauthorized and panic paths over the real network.
	code, _, err := do("GET", "/api/bookmarks", "nope", nil)
	t.add(code, err)
	code, _, err = do("GET", "/debug/panic", "", nil)
	t.add(code, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %v", err)
	}
	for _, r := range results {
		fmt.Println(r)
	}
	codes := make([]int, 0, len(t.status))
	for c := range t.status {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	for _, c := range codes {
		fmt.Printf("status %d %s: %d\n", c, http.StatusText(c), t.status[c])
	}
	fmt.Printf("transport errors: %d\n", len(t.errs))
	total := 0
	for i := 0; i < clients; i++ {
		total += len(store.List(fmt.Sprintf("user%d", i), "", ""))
	}
	fmt.Printf("bookmarks remaining: %d\nserver shut down cleanly\n", total)
	if len(t.errs) > 0 {
		return errors.New(t.errs[0])
	}
	return nil
}

func main() {
	script := flag.String("script", "", "request script to replay")
	self := flag.Bool("selftest", false, "run the loopback self-test")
	clients := flag.Int("clients", 8, "self-test clients")
	requests := flag.Int("requests", 25, "bookmarks created per self-test client")
	flag.Parse()
	var err error
	switch {
	case *script != "":
		err = runScript(*script)
	case *self:
		err = selftest(*clients, *requests)
	default:
		fmt.Fprintln(os.Stderr, "usage: bookmarks -script FILE | -selftest")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bookmarks:", err)
		os.Exit(1)
	}
}
