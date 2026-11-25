// Command regex is a grep-like tool built on its own regular-expression
// engine (a Pike VM with RE2-style leftmost-first semantics). Its -suite
// mode checks the engine against the standard library's regexp package.
//
//	regex [-i] [-n] [-o] [-c] [-v] [-replace repl] pattern [file ...]
//	regex -suite cases.txt
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type options struct {
	fold, lineNum, only, count, invert bool
	replace                            *string
	withName                           bool
}

func grep(re *Regexp, name string, r io.Reader, opt options, w *bufio.Writer) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	matches, lineNo := 0, 0
	prefix := func() {
		if opt.withName {
			w.WriteString(name + ":")
		}
		if opt.lineNum {
			w.WriteString(strconv.Itoa(lineNo) + ":")
		}
	}
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if opt.replace != nil {
			out := re.ReplaceAllString(line, *opt.replace)
			if out != line {
				matches++
			}
			if !opt.count {
				prefix()
				w.WriteString(out + "\n")
			}
			continue
		}
		if opt.only && !opt.invert {
			locs := re.FindAllSubmatchIndex(line, -1)
			if len(locs) > 0 {
				matches++
			}
			if !opt.count {
				for _, loc := range locs {
					prefix()
					w.WriteString(line[loc[0]:loc[1]] + "\n")
				}
			}
			continue
		}
		if re.MatchString(line) != opt.invert {
			matches++
			if !opt.count {
				prefix()
				w.WriteString(line + "\n")
			}
		}
	}
	if opt.count {
		if opt.withName {
			w.WriteString(name + ":")
		}
		fmt.Fprintln(w, matches)
	}
	return matches, sc.Err()
}

func fmtLoc(loc []int) string {
	if loc == nil {
		return "no match"
	}
	parts := make([]string, 0, len(loc)/2)
	for i := 0; i < len(loc); i += 2 {
		parts = append(parts, fmt.Sprintf("%d-%d", loc[i], loc[i+1]))
	}
	return strings.Join(parts, " ")
}

// runSuite reads "pattern<TAB>subject" lines (subject in Go quoted form or
// bare) and compares this engine with the standard library.
func runSuite(path string, w *bufio.Writer) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	disagree := 0
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pat, subj, ok := strings.Cut(line, "\t")
		if !ok {
			return disagree, fmt.Errorf("%s:%d: missing tab", path, i+1)
		}
		if strings.HasPrefix(subj, `"`) {
			if subj, err = strconv.Unquote(subj); err != nil {
				return disagree, fmt.Errorf("%s:%d: bad subject: %w", path, i+1, err)
			}
		}
		mine, myErr := Compile(pat, false)
		std, stdErr := regexp.Compile(pat)
		fmt.Fprintf(w, "%-28q %-22q ", pat, subj)
		if myErr != nil || stdErr != nil {
			verdict := "agree"
			if (myErr == nil) != (stdErr == nil) {
				verdict = "DISAGREE"
				disagree++
			}
			var se *SyntaxError
			if errors.As(myErr, &se) {
				fmt.Fprintf(w, "error@%d %s [%s]\n", se.Offset, se.Msg, verdict)
			} else {
				fmt.Fprintf(w, "compiled (stdlib: %v) [%s]\n", stdErr, verdict)
			}
			continue
		}
		got := mine.FindAllSubmatchIndex(subj, -1)
		want := std.FindAllStringSubmatchIndex(subj, -1)
		same := len(got) == len(want)
		for k := 0; same && k < len(got); k++ {
			same = slices.Equal(got[k], want[k])
		}
		var shown []string
		for _, loc := range got {
			shown = append(shown, "["+fmtLoc(loc)+"]")
		}
		if len(shown) == 0 {
			shown = []string{"no match"}
		}
		verdict := "agree"
		if !same {
			verdict = "DISAGREE"
			disagree++
		}
		fmt.Fprintf(w, "%s [%s]\n", strings.Join(shown, " "), verdict)
	}
	return disagree, nil
}

func main() {
	var opt options
	flag.BoolVar(&opt.fold, "i", false, "case-insensitive matching")
	flag.BoolVar(&opt.lineNum, "n", false, "prefix lines with line numbers")
	flag.BoolVar(&opt.only, "o", false, "print only the matched parts")
	flag.BoolVar(&opt.count, "c", false, "print only a count of matching lines")
	flag.BoolVar(&opt.invert, "v", false, "select non-matching lines")
	repl := flag.String("replace", "", "replace matches with this template ($1, ${name})")
	suite := flag.String("suite", "", "compare the engine with package regexp on a case file")
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "replace" {
			opt.replace = repl
		}
	})

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if *suite != "" {
		n, err := runSuite(*suite, w)
		if err != nil {
			w.Flush()
			fmt.Fprintln(os.Stderr, "regex:", err)
			os.Exit(2)
		}
		fmt.Fprintf(w, "%d disagreements\n", n)
		if n > 0 {
			w.Flush()
			os.Exit(1)
		}
		return
	}
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: regex [-i] [-n] [-o] [-c] [-v] [-replace repl] pattern [file ...]")
		os.Exit(2)
	}
	re, err := Compile(flag.Arg(0), opt.fold)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regex:", err)
		os.Exit(2)
	}
	files := flag.Args()[1:]
	opt.withName = len(files) > 1
	total := 0
	status := 0
	if len(files) == 0 {
		n, err := grep(re, "(standard input)", os.Stdin, opt, w)
		if err != nil {
			fmt.Fprintln(os.Stderr, "regex:", err)
			status = 2
		}
		total += n
	}
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			w.Flush()
			fmt.Fprintln(os.Stderr, "regex:", err)
			status = 2
			continue
		}
		n, err := grep(re, name, f, opt, w)
		f.Close()
		if err != nil {
			w.Flush()
			fmt.Fprintf(os.Stderr, "regex: %s: %v\n", name, err)
			status = 2
		}
		total += n
	}
	w.Flush()
	if status == 0 && total == 0 {
		status = 1
	}
	os.Exit(status)
}
