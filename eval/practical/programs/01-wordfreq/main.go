// Command wordfreq reads text from standard input and prints the most
// frequent words.
//
// Usage: wordfreq [-n N] [-min LEN]
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type entry struct {
	word  string
	count int
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wordfreq [-n N] [-min LEN]")
	os.Exit(2)
}

func parseArgs(args []string) (top, minLen int) {
	top, minLen = 10, 1
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "-min":
			if i+1 >= len(args) {
				usage()
			}
			v, err := strconv.Atoi(args[i+1])
			if err != nil || v < 0 {
				fmt.Fprintf(os.Stderr, "wordfreq: invalid value %q for %s\n", args[i+1], args[i])
				os.Exit(2)
			}
			if args[i] == "-n" {
				top = v
			} else {
				minLen = v
			}
			i++
		default:
			usage()
		}
	}
	return top, minLen
}

// splitWords breaks a line into lowercase words. Apostrophes inside a word
// are kept ("don't"), everything else that is not a letter or digit splits.
func splitWords(line string) []string {
	var words []string
	var cur []rune
	runes := []rune(line)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for i, r := range runes {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur = append(cur, unicode.ToLower(r))
		case r == '\'' && len(cur) > 0 && i+1 < len(runes) && unicode.IsLetter(runes[i+1]):
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return words
}

func main() {
	top, minLen := parseArgs(os.Args[1:])

	counts := make(map[string]int)
	var totalWords, lines int
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines++
		for _, w := range splitWords(sc.Text()) {
			if len([]rune(w)) < minLen {
				continue
			}
			counts[w]++
			totalWords++
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "wordfreq:", err)
		os.Exit(1)
	}

	entries := make([]entry, 0, len(counts))
	for w, c := range counts {
		entries = append(entries, entry{w, c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].word < entries[j].word
	})

	fmt.Printf("lines: %d, words: %d, distinct: %d\n", lines, totalWords, len(counts))
	if len(entries) == 0 {
		return
	}
	width := 0
	for i := 0; i < top && i < len(entries); i++ {
		if n := len(entries[i].word); n > width {
			width = n
		}
	}
	for i := 0; i < top && i < len(entries); i++ {
		e := entries[i]
		pct := 100 * float64(e.count) / float64(totalWords)
		fmt.Printf("%3d. %-*s %6d %6.2f%% %s\n", i+1, width, e.word, e.count, pct,
			strings.Repeat("#", int(pct+0.5)))
	}
}
