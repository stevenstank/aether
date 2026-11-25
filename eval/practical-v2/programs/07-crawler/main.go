// Command crawler crawls a simulated web concurrently and reports pages,
// broken links, and a sitemap.
//
//	crawler [flags] -site FILE START-URL
//	crawler [flags] -synthetic N
package main

import (
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

type sitemapURL struct {
	Loc   string `xml:"loc"`
	Depth int    `xml:"priority,omitempty"`
}

type sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func main() {
	var cfg Config
	flag.IntVar(&cfg.Workers, "workers", 8, "concurrent fetches")
	flag.IntVar(&cfg.MaxDepth, "depth", 3, "maximum link depth")
	flag.IntVar(&cfg.MaxPages, "max", 1000, "maximum pages to visit")
	flag.DurationVar(&cfg.Timeout, "timeout", 50*time.Millisecond, "per-request timeout")
	flag.IntVar(&cfg.Retries, "retries", 2, "retries for transient errors")
	flag.BoolVar(&cfg.SameHost, "same-host", true, "only follow links on the start host")
	siteFile := flag.String("site", "", "site description file")
	synthetic := flag.Int("synthetic", 0, "crawl a generated site with this many pages")
	seed := flag.Uint64("seed", 1, "seed for -synthetic")
	deadline := flag.Duration("deadline", 30*time.Second, "overall crawl deadline")
	showMap := flag.Bool("sitemap", false, "print an XML sitemap")
	flag.Parse()
	cfg.UserAgent = "aether-eval-crawler"

	if cfg.Workers < 1 || cfg.MaxPages < 1 {
		fmt.Fprintln(os.Stderr, "crawler: -workers and -max must be positive")
		os.Exit(2)
	}
	var web *SimWeb
	var start string
	switch {
	case *synthetic > 0:
		web = syntheticSite(*synthetic, *seed)
		start = "https://synth.example/"
	case *siteFile != "" && flag.NArg() == 1:
		f, err := os.Open(*siteFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "crawler:", err)
			os.Exit(1)
		}
		web, err = loadSite(f)
		f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "crawler: %s: %v\n", *siteFile, err)
			os.Exit(1)
		}
		start = flag.Arg(0)
	default:
		fmt.Fprintln(os.Stderr, "usage: crawler [flags] -site FILE START-URL | -synthetic N")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	c := NewCrawler(cfg, web)
	results, err := c.Crawl(ctx, start)
	exit := 0
	if err != nil {
		fmt.Fprintln(os.Stderr, "crawler:", err)
		exit = 1
	}

	inbound := map[string]int{}
	external := map[string]int{}
	for _, r := range results {
		for _, l := range r.Links {
			inbound[l]++
		}
		for _, e := range r.External {
			external[e]++
		}
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 1, ' ', 0)
	fmt.Fprintln(tw, "depth\tstatus\tin\tout\twords\turl\tnote")
	var broken []string
	ok, words := 0, 0
	for _, r := range results {
		note := r.Title
		if r.Err != "" {
			note = "ERROR " + r.Err
		} else if r.Final != r.URL && r.Final != "" {
			note = "-> " + r.Final
		}
		status := fmt.Sprint(r.Status)
		if r.Status == 0 {
			status = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t%d\t%s\t%s\n", r.Depth, status, inbound[r.URL], len(r.Links), r.Words, r.URL, note)
		if r.Status >= 400 || (r.Err != "" && !r.Blocked) {
			broken = append(broken, r.URL)
		}
		if r.Status == 200 {
			ok++
			words += r.Words
		}
	}
	tw.Flush()
	fmt.Printf("\nvisited %d pages (%d ok, %d broken), %d words, %d external links to %d hosts\n",
		len(results), ok, len(broken), words, len(external), countHosts(external))
	fmt.Printf("fetches %d, bytes %d, retries %d\n", c.fetches.Load(), c.bytes.Load(), c.retries.Load())
	if len(broken) > 0 {
		fmt.Println("broken:")
		for _, b := range broken {
			fmt.Println("  " + b)
		}
	}
	top := slices.Sorted(maps.Keys(inbound))
	slices.SortStableFunc(top, func(a, b string) int { return inbound[b] - inbound[a] })
	fmt.Println("most linked:")
	for _, u := range top[:min(5, len(top))] {
		fmt.Printf("  %4d %s\n", inbound[u], u)
	}
	if *showMap {
		sm := sitemap{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		for _, r := range results {
			if r.Status == 200 && r.Final == r.URL {
				sm.URLs = append(sm.URLs, sitemapURL{Loc: r.URL, Depth: r.Depth})
			}
		}
		out, err := xml.MarshalIndent(sm, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "crawler:", err)
			os.Exit(1)
		}
		fmt.Println(xml.Header + string(out))
	}
	os.Exit(exit)
}

func countHosts(urls map[string]int) int {
	hosts := map[string]bool{}
	for u := range urls {
		rest := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
		h, _, _ := strings.Cut(rest, "/")
		hosts[h] = true
	}
	return len(hosts)
}
