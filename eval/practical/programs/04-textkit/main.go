// Command textkit runs text-processing commands read from standard input,
// one per line: "<command> <arguments>". All operations are Unicode-aware.
//
//	reverse TEXT          reverse by characters
//	palindrome TEXT       is TEXT a palindrome (letters and digits only, case-folded)
//	anagrams W1 W2 ...    group words that are anagrams of each other
//	distance A B          Levenshtein distance
//	lcs A B               a longest common subsequence
//	find PATTERN TEXT     character offsets of PATTERN in TEXT (KMP)
//	rle TEXT / unrle TEXT run-length encoding
//	caesar N TEXT         rotate Latin letters by N
//	wrap N TEXT           greedy word wrap at width N
//	title TEXT            title-case each word
//	stats TEXT            bytes, runes, letters, digits, spaces, other
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func isPalindrome(s string) bool {
	var norm []rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			norm = append(norm, unicode.ToLower(r))
		}
	}
	for i, j := 0, len(norm)-1; i < j; i, j = i+1, j-1 {
		if norm[i] != norm[j] {
			return false
		}
	}
	return true
}

func anagramKey(w string) string {
	r := []rune(strings.ToLower(w))
	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
	return string(r)
}

func anagrams(words []string) [][]string {
	groups := map[string][]string{}
	var order []string
	for _, w := range words {
		k := anagramKey(w)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], w)
	}
	var out [][]string
	for _, k := range order {
		if len(groups[k]) > 1 {
			out = append(out, groups[k])
		}
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func lcs(a, b string) string {
	ra, rb := []rune(a), []rune(b)
	dp := make([][]int, len(ra)+1)
	for i := range dp {
		dp[i] = make([]int, len(rb)+1)
	}
	for i := len(ra) - 1; i >= 0; i-- {
		for j := len(rb) - 1; j >= 0; j-- {
			if ra[i] == rb[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var out []rune
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		switch {
		case ra[i] == rb[j]:
			out = append(out, ra[i])
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return string(out)
}

// kmpFind returns the rune offsets of every (possibly overlapping)
// occurrence of pat in text.
func kmpFind(pat, text string) []int {
	p, t := []rune(pat), []rune(text)
	if len(p) == 0 {
		return nil
	}
	fail := make([]int, len(p))
	for i, k := 1, 0; i < len(p); i++ {
		for k > 0 && p[i] != p[k] {
			k = fail[k-1]
		}
		if p[i] == p[k] {
			k++
		}
		fail[i] = k
	}
	var hits []int
	for i, k := 0, 0; i < len(t); i++ {
		for k > 0 && t[i] != p[k] {
			k = fail[k-1]
		}
		if t[i] == p[k] {
			k++
		}
		if k == len(p) {
			hits = append(hits, i-len(p)+1)
			k = fail[k-1]
		}
	}
	return hits
}

func rle(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); {
		j := i
		for j < len(r) && r[j] == r[i] {
			j++
		}
		if n := j - i; n > 1 {
			b.WriteString(strconv.Itoa(n))
		}
		b.WriteRune(r[i])
		i = j
	}
	return b.String()
}

var errBadRLE = errors.New("malformed run-length encoding")

func unrle(s string) (string, error) {
	var b strings.Builder
	n := 0
	digits := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			digits = true
			if n > 1_000_000 {
				return "", fmt.Errorf("%w: run too long", errBadRLE)
			}
			continue
		}
		if !digits {
			n = 1
		} else if n == 0 {
			return "", fmt.Errorf("%w: zero-length run", errBadRLE)
		}
		b.WriteString(strings.Repeat(string(r), n))
		n, digits = 0, false
	}
	if digits {
		return "", fmt.Errorf("%w: trailing count", errBadRLE)
	}
	return b.String(), nil
}

func caesar(n int, s string) string {
	n = ((n % 26) + 26) % 26
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return 'a' + (r-'a'+rune(n))%26
		case r >= 'A' && r <= 'Z':
			return 'A' + (r-'A'+rune(n))%26
		}
		return r
	}, s)
}

func wrap(width int, s string) []string {
	var lines []string
	var cur strings.Builder
	curLen := 0
	for _, w := range strings.Fields(s) {
		wl := utf8.RuneCountInString(w)
		if curLen > 0 && curLen+1+wl > width {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
		}
		if curLen > 0 {
			cur.WriteByte(' ')
			curLen++
		}
		cur.WriteString(w)
		curLen += wl
	}
	if curLen > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func title(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		defer func() { prev = r }()
		if unicode.IsSpace(prev) || prev == '-' {
			return unicode.ToTitle(r)
		}
		return unicode.ToLower(r)
	}, s)
}

type stats struct{ bytes, runes, letters, digits, spaces, other, invalid int }

func textStats(s string) stats {
	st := stats{bytes: len(s)}
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		st.runes++
		switch {
		case r == utf8.RuneError && size == 1:
			st.invalid++
		case unicode.IsLetter(r):
			st.letters++
		case unicode.IsDigit(r):
			st.digits++
		case unicode.IsSpace(r):
			st.spaces++
		default:
			st.other++
		}
	}
	return st
}

func run(cmd, rest string) (string, error) {
	args := strings.Fields(rest)
	need := func(n int) error {
		if len(args) < n {
			return fmt.Errorf("%s: need %d argument(s)", cmd, n)
		}
		return nil
	}
	intArg := func() (int, string, error) {
		n, text, _ := strings.Cut(rest, " ")
		v, err := strconv.Atoi(n)
		if err != nil {
			return 0, "", fmt.Errorf("%s: bad number %q", cmd, n)
		}
		return v, text, nil
	}
	switch cmd {
	case "reverse":
		return reverse(rest), nil
	case "palindrome":
		return strconv.FormatBool(isPalindrome(rest)), nil
	case "anagrams":
		groups := anagrams(args)
		if len(groups) == 0 {
			return "(none)", nil
		}
		parts := make([]string, len(groups))
		for i, g := range groups {
			parts[i] = "[" + strings.Join(g, " ") + "]"
		}
		return strings.Join(parts, " "), nil
	case "distance":
		if err := need(2); err != nil {
			return "", err
		}
		return strconv.Itoa(levenshtein(args[0], args[1])), nil
	case "lcs":
		if err := need(2); err != nil {
			return "", err
		}
		return strconv.Quote(lcs(args[0], args[1])), nil
	case "find":
		pat, text, ok := strings.Cut(rest, " ")
		if !ok {
			return "", errors.New("find: need PATTERN TEXT")
		}
		return fmt.Sprint(kmpFind(pat, text)), nil
	case "rle":
		return rle(rest), nil
	case "unrle":
		return unrle(rest)
	case "caesar":
		n, text, err := intArg()
		if err != nil {
			return "", err
		}
		return caesar(n, text), nil
	case "wrap":
		n, text, err := intArg()
		if err != nil {
			return "", err
		}
		if n <= 0 {
			return "", fmt.Errorf("wrap: width must be positive, got %d", n)
		}
		return strings.Join(wrap(n, text), "\n"), nil
	case "title":
		return title(rest), nil
	case "stats":
		st := textStats(rest)
		return fmt.Sprintf("%+v", st), nil
	}
	return "", fmt.Errorf("unknown command %q", cmd)
}

func main() {
	rd := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	errs := 0
	for n := 1; ; n++ {
		line, err := rd.ReadString('\n')
		if err != nil && err != io.EOF {
			fmt.Fprintln(os.Stderr, "textkit:", err)
			os.Exit(2)
		}
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			cmd, rest, _ := strings.Cut(line, " ")
			out, cerr := run(cmd, rest)
			if cerr != nil {
				errs++
				fmt.Fprintf(w, "%d: error: %v\n", n, cerr)
			} else {
				fmt.Fprintf(w, "%d: %s\n", n, out)
			}
		}
		if err == io.EOF {
			break
		}
	}
	if errs > 0 {
		w.Flush()
		os.Exit(1)
	}
}
