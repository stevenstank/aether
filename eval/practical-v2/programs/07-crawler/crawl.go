package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Workers   int
	MaxDepth  int
	MaxPages  int
	Timeout   time.Duration
	Retries   int
	SameHost  bool
	UserAgent string
}

type Result struct {
	URL      string
	Final    string
	Depth    int
	Status   int
	Title    string
	Words    int
	Links    []string
	External []string
	Err      string
	Attempts int
	Blocked  bool
}

type robots struct {
	once     sync.Once
	disallow []string
}

type Crawler struct {
	cfg     Config
	fetch   Fetcher
	mu      sync.Mutex
	robots  map[string]*robots
	fetches atomic.Int64
	bytes   atomic.Int64
	retries atomic.Int64
}

func NewCrawler(cfg Config, f Fetcher) *Crawler {
	return &Crawler{cfg: cfg, fetch: f, robots: map[string]*robots{}}
}

var trackingParams = []string{"ref", "utm_source", "utm_medium", "utm_campaign"}

// normalize resolves ref against base and canonicalizes it, or returns ""
// for links that are not crawlable.
func normalize(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	u = base.ResolveReference(u)
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	q := u.Query()
	for _, p := range trackingParams {
		q.Del(p)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// extract returns the title, word count, and href targets of an HTML body.
func extract(body string) (title string, words int, hrefs []string) {
	lower := strings.ToLower(body)
	if i := strings.Index(lower, "<title>"); i >= 0 {
		if j := strings.Index(lower[i:], "</title>"); j >= 0 {
			title = strings.TrimSpace(html.UnescapeString(body[i+7 : i+j]))
		}
	}
	var text strings.Builder
	for i := 0; i < len(body); {
		if body[i] != '<' {
			j := strings.IndexByte(body[i:], '<')
			if j < 0 {
				j = len(body) - i
			}
			text.WriteString(body[i : i+j])
			text.WriteByte(' ')
			i += j
			continue
		}
		end := strings.IndexByte(body[i:], '>')
		if end < 0 {
			break
		}
		tag := body[i+1 : i+end]
		i += end + 1
		name, attrs, _ := strings.Cut(tag, " ")
		if !strings.EqualFold(name, "a") {
			continue
		}
		la := strings.ToLower(attrs)
		k := strings.Index(la, "href=")
		if k < 0 {
			continue
		}
		v := attrs[k+5:]
		if v == "" {
			continue
		}
		var val string
		if q := v[0]; q == '"' || q == '\'' {
			if e := strings.IndexByte(v[1:], q); e >= 0 {
				val = v[1 : 1+e]
			}
		} else {
			val, _, _ = strings.Cut(v, " ")
		}
		hrefs = append(hrefs, html.UnescapeString(val))
	}
	words = len(strings.Fields(html.UnescapeString(text.String())))
	return title, words, hrefs
}

func (c *Crawler) allowed(ctx context.Context, u *url.URL) bool {
	c.mu.Lock()
	r := c.robots[u.Host]
	if r == nil {
		r = &robots{}
		c.robots[u.Host] = r
	}
	c.mu.Unlock()
	r.once.Do(func() {
		robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
		resp, err := c.get(ctx, robotsURL)
		if err != nil || resp.Status != 200 {
			return
		}
		applies := false
		for _, line := range strings.Split(resp.Body, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
			switch k {
			case "user-agent":
				applies = v == "*" || strings.EqualFold(v, c.cfg.UserAgent)
			case "disallow":
				if applies && v != "" {
					r.disallow = append(r.disallow, v)
				}
			}
		}
	})
	for _, d := range r.disallow {
		if strings.HasPrefix(u.Path, d) {
			return false
		}
	}
	return true
}

// get fetches with a timeout and retries transient errors.
func (c *Crawler) get(ctx context.Context, u string) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			c.retries.Add(1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Millisecond):
			}
		}
		fctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
		resp, err := c.fetch.Fetch(fctx, u)
		cancel()
		c.fetches.Add(1)
		if err == nil {
			c.bytes.Add(int64(len(resp.Body)))
			return resp, nil
		}
		lastErr = err
		if !errors.Is(err, ErrTransient) {
			break
		}
	}
	return nil, lastErr
}

func (c *Crawler) visit(ctx context.Context, raw string, depth int) *Result {
	res := &Result{URL: raw, Depth: depth}
	cur := raw
	for hops := 0; ; hops++ {
		u, _ := url.Parse(cur)
		if !c.allowed(ctx, u) {
			res.Blocked = true
			res.Err = "blocked by robots.txt"
			return res
		}
		res.Attempts++
		resp, err := c.get(ctx, cur)
		if err != nil {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				res.Err = "timeout"
			case errors.Is(err, ErrNoHost):
				res.Err = "dns: " + err.Error()
			default:
				res.Err = err.Error()
			}
			return res
		}
		res.Status = resp.Status
		if resp.Status >= 300 && resp.Status < 400 && resp.Location != "" {
			if hops >= 5 {
				res.Err = "too many redirects"
				return res
			}
			next := normalize(u, resp.Location)
			if next == "" {
				res.Err = "bad redirect"
				return res
			}
			cur = next
			continue
		}
		res.Final = cur
		if resp.Status != 200 || !strings.HasPrefix(resp.Type, "text/html") {
			return res
		}
		var hrefs []string
		res.Title, res.Words, hrefs = extract(resp.Body)
		seen := map[string]bool{}
		for _, h := range hrefs {
			n := normalize(u, h)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			nu, _ := url.Parse(n)
			if c.cfg.SameHost && nu.Host != u.Host {
				res.External = append(res.External, n)
			} else {
				res.Links = append(res.Links, n)
			}
		}
		return res
	}
}

// Crawl visits pages breadth first, one depth level at a time, with a
// pool of workers per level. Within a level, URLs are visited in sorted
// order, so the set of visited pages does not depend on scheduling.
func (c *Crawler) Crawl(ctx context.Context, start string) ([]*Result, error) {
	seen := map[string]bool{start: true}
	frontier := []string{start}
	var all []*Result
	for depth := 0; depth <= c.cfg.MaxDepth && len(frontier) > 0; depth++ {
		slices.Sort(frontier)
		if room := c.cfg.MaxPages - len(all); len(frontier) > room {
			frontier = frontier[:room]
		}
		jobs := make(chan string)
		results := make(chan *Result, c.cfg.Workers)
		var wg sync.WaitGroup
		for w := 0; w < c.cfg.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for u := range jobs {
					results <- c.visit(ctx, u, depth)
				}
			}()
		}
		go func() {
			defer close(jobs)
			for _, u := range frontier {
				select {
				case jobs <- u:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() {
			wg.Wait()
			close(results)
		}()
		var level []*Result
		for r := range results {
			level = append(level, r)
		}
		if err := ctx.Err(); err != nil {
			return all, fmt.Errorf("crawl stopped at depth %d: %w", depth, err)
		}
		slices.SortFunc(level, func(a, b *Result) int { return strings.Compare(a.URL, b.URL) })
		var next []string
		for _, r := range level {
			all = append(all, r)
			for _, l := range r.Links {
				if !seen[l] {
					seen[l] = true
					next = append(next, l)
				}
			}
		}
		frontier = next
		if len(all) >= c.cfg.MaxPages {
			break
		}
	}
	return all, nil
}
