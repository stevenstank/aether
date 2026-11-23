// Command logstat analyzes access logs from standard input in parallel.
//
// Each line is: TIMESTAMP LEVEL SERVICE LATENCY_MS STATUS PATH
// for example: 2026-03-01T10:15:02Z INFO checkout 35 200 /cart
//
// Usage: logstat [-workers N] [-batch N] [-maxerr N]
//
// The report is independent of the number of workers. If more than maxerr
// lines are malformed, processing stops early and the exit status is 1.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type entry struct {
	at      time.Time
	level   string
	service string
	latency int
	status  int
	path    string
}

type batch struct {
	first int // line number of lines[0]
	lines []string
}

type lineError struct {
	line int
	err  error
}

// partial holds one worker's statistics for one batch.
type partial struct {
	services map[string]*svcStats
	hours    map[time.Time]int
	levels   map[string]int
	paths    map[string]int
	lines    int
}

type svcStats struct {
	count     int
	errors    int // status >= 500
	latencies []int
}

func newPartial() *partial {
	return &partial{
		services: map[string]*svcStats{},
		hours:    map[time.Time]int{},
		levels:   map[string]int{},
		paths:    map[string]int{},
	}
}

func (p *partial) add(e entry) {
	s := p.services[e.service]
	if s == nil {
		s = &svcStats{}
		p.services[e.service] = s
	}
	s.count++
	if e.status >= 500 {
		s.errors++
	}
	s.latencies = append(s.latencies, e.latency)
	p.hours[e.at.Truncate(time.Hour)]++
	p.levels[e.level]++
	p.paths[e.path]++
	p.lines++
}

func (p *partial) merge(o *partial) {
	for name, os := range o.services {
		s := p.services[name]
		if s == nil {
			s = &svcStats{}
			p.services[name] = s
		}
		s.count += os.count
		s.errors += os.errors
		s.latencies = append(s.latencies, os.latencies...)
	}
	for h, n := range o.hours {
		p.hours[h] += n
	}
	for l, n := range o.levels {
		p.levels[l] += n
	}
	for path, n := range o.paths {
		p.paths[path] += n
	}
	p.lines += o.lines
}

var validLevels = map[string]bool{"DEBUG": true, "INFO": true, "WARN": true, "ERROR": true}

func parseLine(s string) (entry, error) {
	f := strings.Fields(s)
	if len(f) != 6 {
		return entry{}, fmt.Errorf("want 6 fields, got %d", len(f))
	}
	at, err := time.Parse(time.RFC3339, f[0])
	if err != nil {
		return entry{}, fmt.Errorf("bad timestamp %q", f[0])
	}
	if !validLevels[f[1]] {
		return entry{}, fmt.Errorf("bad level %q", f[1])
	}
	lat, err := strconv.Atoi(f[3])
	if err != nil || lat < 0 {
		return entry{}, fmt.Errorf("bad latency %q", f[3])
	}
	st, err := strconv.Atoi(f[4])
	if err != nil || st < 100 || st > 599 {
		return entry{}, fmt.Errorf("bad status %q", f[4])
	}
	return entry{at.UTC(), f[1], f[2], lat, st, f[5]}, nil
}

var errTooMany = errors.New("too many malformed lines")

func percentile(sorted []int, p float64) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p*float64(len(sorted)-1) + 0.5)
	return sorted[idx]
}

func main() {
	workers, batchSize, maxErr := 4, 256, 100
	args := os.Args[1:]
	for len(args) >= 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil || v <= 0 {
			fmt.Fprintf(os.Stderr, "logstat: bad value %q for %s\n", args[1], args[0])
			os.Exit(2)
		}
		switch args[0] {
		case "-workers":
			workers = v
		case "-batch":
			batchSize = v
		case "-maxerr":
			maxErr = v
		default:
			fmt.Fprintf(os.Stderr, "logstat: unknown flag %s\n", args[0])
			os.Exit(2)
		}
		args = args[2:]
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: logstat [-workers N] [-batch N] [-maxerr N]")
		os.Exit(2)
	}

	batches := make(chan batch, workers)
	results := make(chan *partial, workers)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }

	var (
		mu       sync.Mutex
		badLines []lineError
		nBad     atomic.Int64
		nRead    atomic.Int64
	)

	// Reader.
	go func() {
		defer close(batches)
		sc := bufio.NewScanner(os.Stdin)
		b := batch{first: 1}
		n := 0
		for sc.Scan() {
			n++
			nRead.Add(1)
			b.lines = append(b.lines, sc.Text())
			if len(b.lines) == batchSize {
				select {
				case batches <- b:
				case <-done:
					return
				}
				b = batch{first: n + 1}
			}
		}
		if len(b.lines) > 0 {
			select {
			case batches <- b:
			case <-done:
			}
		}
	}()

	// Workers.
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range batches {
				p := newPartial()
				for i, line := range b.lines {
					if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
						continue
					}
					e, err := parseLine(line)
					if err != nil {
						mu.Lock()
						badLines = append(badLines, lineError{b.first + i, err})
						mu.Unlock()
						if nBad.Add(1) > int64(maxErr) {
							stop()
						}
						continue
					}
					p.add(e)
				}
				select {
				case results <- p:
				case <-done:
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	total := newPartial()
	for p := range results {
		total.merge(p)
	}

	select {
	case <-done:
		fmt.Fprintf(os.Stderr, "logstat: %v (limit %d)\n", errTooMany, maxErr)
		os.Exit(1)
	default:
	}

	sort.Slice(badLines, func(i, j int) bool { return badLines[i].line < badLines[j].line })
	fmt.Printf("lines: %d read, %d parsed, %d malformed\n", nRead.Load(), total.lines, len(badLines))
	for i, b := range badLines {
		if i == 5 {
			fmt.Printf("  ... and %d more\n", len(badLines)-5)
			break
		}
		fmt.Printf("  line %d: %v\n", b.line, b.err)
	}

	names := make([]string, 0, len(total.services))
	for n := range total.services {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("\n%-12s %7s %7s %6s %6s %6s %6s\n", "SERVICE", "COUNT", "ERR%", "P50", "P90", "P99", "MAX")
	for _, n := range names {
		s := total.services[n]
		sort.Ints(s.latencies)
		fmt.Printf("%-12s %7d %6.2f%% %6d %6d %6d %6d\n", n, s.count,
			100*float64(s.errors)/float64(s.count),
			percentile(s.latencies, 0.50), percentile(s.latencies, 0.90),
			percentile(s.latencies, 0.99), s.latencies[len(s.latencies)-1])
	}

	hours := make([]time.Time, 0, len(total.hours))
	for h := range total.hours {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	fmt.Println("\nrequests per hour:")
	peak := 0
	for _, h := range hours {
		peak = max(peak, total.hours[h])
	}
	for _, h := range hours {
		n := total.hours[h]
		fmt.Printf("  %s %6d %s\n", h.Format("2006-01-02 15:04"), n, strings.Repeat("*", n*40/peak))
	}

	type kv struct {
		k string
		v int
	}
	var paths []kv
	for p, n := range total.paths {
		paths = append(paths, kv{p, n})
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].v != paths[j].v {
			return paths[i].v > paths[j].v
		}
		return paths[i].k < paths[j].k
	})
	fmt.Println("\ntop paths:")
	for i := 0; i < 5 && i < len(paths); i++ {
		fmt.Printf("  %-16s %d\n", paths[i].k, paths[i].v)
	}
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	fmt.Print("\nlevels:")
	for _, l := range levels {
		fmt.Printf(" %s=%d", l, total.levels[l])
	}
	fmt.Println()
}
