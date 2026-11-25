// Command logstat summarizes HTTP access logs: request counts, status
// classes, latency percentiles, the busiest endpoints, and traffic per
// time bucket.
//
// Each log line looks like:
//
//	2026-03-14T10:22:31.123Z 10.0.0.7 GET /api/users/42 200 1532 12.5ms "curl/8.0"
//
// Usage: logstat [flags] [file ...]   (reads stdin when no file is given)
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

var lineRE = regexp.MustCompile(`^(\S+) (\d{1,3}(?:\.\d{1,3}){3}) ([A-Z]+) (\S+) (\d{3}) (\d+|-) (\S+) "([^"]*)"$`)

// Normalizes path segments that look like identifiers so that
// /api/users/42 and /api/users/43 count as one endpoint.
var idSegment = regexp.MustCompile(`/(\d+|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(/|$)`)

type Entry struct {
	When    time.Time
	IP      string
	Method  string
	Path    string
	Status  int
	Bytes   int64
	Latency time.Duration
	Agent   string
}

type ParseError struct {
	File string
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
}

func parseLine(s string) (Entry, error) {
	m := lineRE.FindStringSubmatch(s)
	if m == nil {
		return Entry{}, errors.New("malformed line")
	}
	when, err := time.Parse(time.RFC3339Nano, m[1])
	if err != nil {
		return Entry{}, fmt.Errorf("bad timestamp %q", m[1])
	}
	status, _ := strconv.Atoi(m[5])
	if status < 100 || status > 599 {
		return Entry{}, fmt.Errorf("bad status %d", status)
	}
	var size int64
	if m[6] != "-" {
		size, err = strconv.ParseInt(m[6], 10, 64)
		if err != nil {
			return Entry{}, fmt.Errorf("bad size %q", m[6])
		}
	}
	lat, err := time.ParseDuration(m[7])
	if err != nil {
		return Entry{}, fmt.Errorf("bad latency: %v", err)
	}
	return Entry{When: when.UTC(), IP: m[2], Method: m[3], Path: m[4], Status: status, Bytes: size, Latency: lat, Agent: m[8]}, nil
}

func endpoint(method, path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for {
		p := idSegment.ReplaceAllString(path, "/:id$2")
		if p == path {
			break
		}
		path = p
	}
	return method + " " + path
}

type endpointStats struct {
	Name      string  `json:"endpoint"`
	Count     int     `json:"count"`
	Errors    int     `json:"errors"`
	Bytes     int64   `json:"bytes"`
	P50       float64 `json:"p50_ms"`
	P95       float64 `json:"p95_ms"`
	latencies []float64
}

type bucket struct {
	Start time.Time `json:"start"`
	Count int       `json:"count"`
	Bytes int64     `json:"bytes"`
}

type Report struct {
	Lines      int                `json:"lines"`
	Requests   int                `json:"requests"`
	Invalid    int                `json:"invalid"`
	Filtered   int                `json:"filtered"`
	First      time.Time          `json:"first"`
	Last       time.Time          `json:"last"`
	Status     map[string]int     `json:"status_classes"`
	Methods    map[string]int     `json:"methods"`
	UniqueIPs  int                `json:"unique_ips"`
	TotalBytes int64              `json:"total_bytes"`
	LatencyMS  map[string]float64 `json:"latency_ms"`
	Top        []*endpointStats   `json:"top_endpoints"`
	Buckets    []bucket           `json:"buckets"`
}

// percentile uses the nearest-rank method on sorted data.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

type options struct {
	top      int
	bucket   time.Duration
	since    time.Time
	until    time.Time
	asJSON   bool
	maxShown int
}

type analyzer struct {
	opt       options
	rep       Report
	ips       map[string]struct{}
	eps       map[string]*endpointStats
	buckets   map[int64]*bucket
	latencies []float64
	shown     int
}

func newAnalyzer(opt options) *analyzer {
	return &analyzer{
		opt:     opt,
		rep:     Report{Status: map[string]int{}, Methods: map[string]int{}, LatencyMS: map[string]float64{}},
		ips:     map[string]struct{}{},
		eps:     map[string]*endpointStats{},
		buckets: map[int64]*bucket{},
	}
}

func (a *analyzer) consume(name string, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		a.rep.Lines++
		text := strings.TrimRight(sc.Text(), "\r")
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		e, err := parseLine(text)
		if err != nil {
			a.rep.Invalid++
			if a.shown < a.opt.maxShown {
				a.shown++
				fmt.Fprintln(os.Stderr, "warning:", &ParseError{File: name, Line: n, Msg: err.Error()})
			}
			continue
		}
		if (!a.opt.since.IsZero() && e.When.Before(a.opt.since)) || (!a.opt.until.IsZero() && !e.When.Before(a.opt.until)) {
			a.rep.Filtered++
			continue
		}
		a.add(e)
	}
	return sc.Err()
}

func (a *analyzer) add(e Entry) {
	r := &a.rep
	r.Requests++
	if r.First.IsZero() || e.When.Before(r.First) {
		r.First = e.When
	}
	if e.When.After(r.Last) {
		r.Last = e.When
	}
	r.Status[fmt.Sprintf("%dxx", e.Status/100)]++
	r.Methods[e.Method]++
	r.TotalBytes += e.Bytes
	a.ips[e.IP] = struct{}{}
	l := ms(e.Latency)
	a.latencies = append(a.latencies, l)

	key := endpoint(e.Method, e.Path)
	st := a.eps[key]
	if st == nil {
		st = &endpointStats{Name: key}
		a.eps[key] = st
	}
	st.Count++
	st.Bytes += e.Bytes
	if e.Status >= 500 {
		st.Errors++
	}
	st.latencies = append(st.latencies, l)

	k := e.When.Truncate(a.opt.bucket).Unix()
	b := a.buckets[k]
	if b == nil {
		b = &bucket{Start: e.When.Truncate(a.opt.bucket)}
		a.buckets[k] = b
	}
	b.Count++
	b.Bytes += e.Bytes
}

func (a *analyzer) finish() *Report {
	r := &a.rep
	r.UniqueIPs = len(a.ips)
	sort.Float64s(a.latencies)
	if len(a.latencies) > 0 {
		sum := 0.0
		for _, v := range a.latencies {
			sum += v
		}
		r.LatencyMS["min"] = a.latencies[0]
		r.LatencyMS["mean"] = math.Round(sum/float64(len(a.latencies))*1000) / 1000
		r.LatencyMS["p50"] = percentile(a.latencies, 50)
		r.LatencyMS["p90"] = percentile(a.latencies, 90)
		r.LatencyMS["p99"] = percentile(a.latencies, 99)
		r.LatencyMS["max"] = a.latencies[len(a.latencies)-1]
	}
	all := make([]*endpointStats, 0, len(a.eps))
	for _, st := range a.eps {
		sort.Float64s(st.latencies)
		st.P50 = percentile(st.latencies, 50)
		st.P95 = percentile(st.latencies, 95)
		all = append(all, st)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Count != all[j].Count {
			return all[i].Count > all[j].Count
		}
		return all[i].Name < all[j].Name
	})
	if len(all) > a.opt.top {
		all = all[:a.opt.top]
	}
	r.Top = all
	keys := make([]int64, 0, len(a.buckets))
	for k := range a.buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		r.Buckets = append(r.Buckets, *a.buckets[k])
	}
	return r
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func printText(w io.Writer, r *Report, bucketSize time.Duration) {
	fmt.Fprintf(w, "lines: %d  requests: %d  invalid: %d  filtered: %d\n", r.Lines, r.Requests, r.Invalid, r.Filtered)
	if r.Requests == 0 {
		fmt.Fprintln(w, "no requests")
		return
	}
	span := r.Last.Sub(r.First)
	fmt.Fprintf(w, "window: %s .. %s (%v)\n", r.First.Format(time.RFC3339), r.Last.Format(time.RFC3339), span)
	rate := 0.0
	if span > 0 {
		rate = float64(r.Requests) / span.Seconds()
	}
	fmt.Fprintf(w, "unique clients: %d  transferred: %s  rate: %.2f req/s\n", r.UniqueIPs, humanBytes(r.TotalBytes), rate)

	classes := make([]string, 0, len(r.Status))
	for k := range r.Status {
		classes = append(classes, k)
	}
	sort.Strings(classes)
	fmt.Fprint(w, "status:")
	for _, c := range classes {
		fmt.Fprintf(w, " %s=%d (%.1f%%)", c, r.Status[c], 100*float64(r.Status[c])/float64(r.Requests))
	}
	fmt.Fprintln(w)
	methods := make([]string, 0, len(r.Methods))
	for k := range r.Methods {
		methods = append(methods, k)
	}
	sort.Slice(methods, func(i, j int) bool {
		if r.Methods[methods[i]] != r.Methods[methods[j]] {
			return r.Methods[methods[i]] > r.Methods[methods[j]]
		}
		return methods[i] < methods[j]
	})
	fmt.Fprintf(w, "methods: %s\n", strings.Join(methods, ", "))
	l := r.LatencyMS
	fmt.Fprintf(w, "latency ms: min %.3f  mean %.3f  p50 %.3f  p90 %.3f  p99 %.3f  max %.3f\n",
		l["min"], l["mean"], l["p50"], l["p90"], l["p99"], l["max"])

	fmt.Fprintln(w, "\ntop endpoints:")
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "count\terrors\tp50 ms\tp95 ms\tbytes\t endpoint\t")
	for _, e := range r.Top {
		fmt.Fprintf(tw, "%d\t%d\t%.2f\t%.2f\t%s\t %s\t\n", e.Count, e.Errors, e.P50, e.P95, humanBytes(e.Bytes), e.Name)
	}
	tw.Flush()

	fmt.Fprintf(w, "\ntraffic per %v:\n", bucketSize)
	peak := 0
	for _, b := range r.Buckets {
		peak = max(peak, b.Count)
	}
	for _, b := range r.Buckets {
		bar := strings.Repeat("#", (b.Count*40+peak-1)/peak)
		fmt.Fprintf(w, "%s %6d %s\n", b.Start.Format("2006-01-02 15:04"), b.Count, bar)
	}
}

func parseTimeFlag(name, v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("-%s: cannot parse %q as a time", name, v)
}

func main() {
	fs := flag.NewFlagSet("logstat", flag.ContinueOnError)
	top := fs.Int("top", 10, "number of endpoints to list")
	bucketSize := fs.Duration("bucket", time.Hour, "time bucket size")
	since := fs.String("since", "", "ignore requests before this time")
	until := fs.String("until", "", "ignore requests at or after this time")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	opt := options{top: *top, bucket: *bucketSize, asJSON: *asJSON, maxShown: 5}
	if opt.top < 0 || opt.bucket <= 0 {
		fmt.Fprintln(os.Stderr, "logstat: -top must be >= 0 and -bucket > 0")
		os.Exit(2)
	}
	var err error
	if opt.since, err = parseTimeFlag("since", *since); err == nil {
		opt.until, err = parseTimeFlag("until", *until)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "logstat:", err)
		os.Exit(2)
	}

	a := newAnalyzer(opt)
	files := fs.Args()
	status := 0
	if len(files) == 0 {
		if err := a.consume("<stdin>", os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "logstat:", err)
			os.Exit(1)
		}
	}
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			var pe *os.PathError
			if errors.As(err, &pe) {
				fmt.Fprintf(os.Stderr, "logstat: skipping %s: %v\n", pe.Path, pe.Err)
			} else {
				fmt.Fprintln(os.Stderr, "logstat:", err)
			}
			status = 1
			continue
		}
		err = a.consume(name, f)
		f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "logstat: %s: %v\n", name, err)
			status = 1
		}
	}
	if a.shown < a.rep.Invalid {
		fmt.Fprintf(os.Stderr, "warning: %d more invalid lines not shown\n", a.rep.Invalid-a.shown)
	}
	rep := a.finish()
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	if opt.asJSON {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "logstat:", err)
			os.Exit(1)
		}
		out.Write(b)
		out.WriteString("\n")
	} else {
		printText(out, rep, opt.bucket)
	}
	if status != 0 {
		out.Flush()
		os.Exit(status)
	}
}
