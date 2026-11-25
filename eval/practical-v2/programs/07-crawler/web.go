package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Response is what a fetch returns.
type Response struct {
	URL      string
	Status   int
	Location string // for redirects
	Body     string
	Type     string
}

// Fetcher retrieves a URL. Implementations must honor ctx.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) (*Response, error)
}

var ErrNoHost = errors.New("no such host")

type page struct {
	status   int
	location string
	latency  time.Duration
	body     string
	ctype    string
	failures int // fail this many times before succeeding
}

// SimWeb is an in-memory web with simulated latency and failures.
type SimWeb struct {
	pages map[string]*page
	hosts map[string]bool
	tries map[string]int
	mu    chan struct{} // a one-slot semaphore used as a mutex
}

func newSimWeb() *SimWeb {
	w := &SimWeb{pages: map[string]*page{}, hosts: map[string]bool{}, tries: map[string]int{}, mu: make(chan struct{}, 1)}
	return w
}

func (w *SimWeb) add(u string, p *page) {
	w.pages[u] = p
	if pu, err := url.Parse(u); err == nil {
		w.hosts[pu.Host] = true
	}
}

var ErrTransient = errors.New("connection reset by peer")

func (w *SimWeb) Fetch(ctx context.Context, rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !w.hosts[u.Host] {
		return nil, fmt.Errorf("dial %s: %w", u.Host, ErrNoHost)
	}
	p, ok := w.pages[rawURL]
	if !ok {
		return &Response{URL: rawURL, Status: 404, Body: "not found", Type: "text/plain"}, nil
	}
	w.mu <- struct{}{}
	w.tries[rawURL]++
	n := w.tries[rawURL]
	<-w.mu
	if n <= p.failures {
		return nil, ErrTransient
	}
	t := time.NewTimer(p.latency)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.C:
	}
	return &Response{URL: rawURL, Status: p.status, Location: p.location, Body: p.body, Type: p.ctype}, nil
}

// loadSite parses the site description format:
//
//	== URL STATUS [latency=DUR] [type=MIME] [fail=N] [-> LOCATION]
//	body lines...
func loadSite(r io.Reader) (*SimWeb, error) {
	w := newSimWeb()
	sc := bufio.NewScanner(r)
	var cur *page
	var body []string
	var curURL string
	lineNo := 0
	finish := func() {
		if cur != nil {
			cur.body = strings.Join(body, "\n")
			w.add(curURL, cur)
		}
	}
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if !strings.HasPrefix(line, "== ") {
			if cur == nil {
				if strings.TrimSpace(line) == "" {
					continue
				}
				return nil, fmt.Errorf("line %d: content before first page header", lineNo)
			}
			body = append(body, line)
			continue
		}
		finish()
		f := strings.Fields(line[3:])
		if len(f) < 2 {
			return nil, fmt.Errorf("line %d: want '== URL STATUS'", lineNo)
		}
		status, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: bad status %q", lineNo, f[1])
		}
		cur = &page{status: status, latency: time.Millisecond, ctype: "text/html"}
		curURL, body = f[0], nil
		for i := 2; i < len(f); i++ {
			k, v, _ := strings.Cut(f[i], "=")
			switch {
			case f[i] == "->" && i+1 < len(f):
				cur.location = f[i+1]
				i++
			case k == "latency":
				if cur.latency, err = time.ParseDuration(v); err != nil {
					return nil, fmt.Errorf("line %d: %v", lineNo, err)
				}
			case k == "type":
				cur.ctype = v
			case k == "fail":
				cur.failures, _ = strconv.Atoi(v)
			default:
				return nil, fmt.Errorf("line %d: unknown attribute %q", lineNo, f[i])
			}
		}
	}
	finish()
	return w, sc.Err()
}

// syntheticSite builds a deterministic site of n pages with a seeded PRNG.
func syntheticSite(n int, seed uint64) *SimWeb {
	w := newSimWeb()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	words := []string{"alpha", "beta", "gamma", "delta", "orbit", "quartz", "river", "signal", "tundra", "vector"}
	for i := 0; i < n; i++ {
		var b strings.Builder
		fmt.Fprintf(&b, "<html><head><title>Page %d</title></head><body>\n", i)
		for k := 0; k < 20+rng.IntN(40); k++ {
			b.WriteString(words[rng.IntN(len(words))])
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "<a href=\"/p%d\">next</a>\n", (i+1)%n)
		links := rng.IntN(6)
		for k := 0; k < links; k++ {
			target := rng.IntN(n + n/10) // some links point past the end: 404s
			switch rng.IntN(10) {
			case 0:
				fmt.Fprintf(&b, "<a href=\"https://elsewhere.example/p%d\">ext</a>\n", target)
			case 1:
				fmt.Fprintf(&b, "<a href=\"../p%d?ref=%d#top\">rel</a>\n", target, i)
			default:
				fmt.Fprintf(&b, "<a href=\"/p%d\">p%d</a>\n", target, target)
			}
		}
		b.WriteString("</body></html>")
		u := fmt.Sprintf("https://synth.example/p%d", i)
		w.add(u, &page{status: 200, latency: time.Duration(rng.IntN(300)) * time.Microsecond, body: b.String(), ctype: "text/html"})
	}
	w.add("https://synth.example/", &page{status: 301, location: "/p0", latency: time.Microsecond})
	w.add("https://synth.example/robots.txt", &page{status: 200, body: "User-agent: *\nDisallow: /p13", ctype: "text/plain"})
	return w
}
