package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runeStrings are the strings TestRuneConversions decodes: every length
// of encoding, its boundaries, and invalid UTF-8 of each kind: bytes never
// valid, overlong forms, surrogates, runes above U+10FFFF, and sequences
// cut short.
var runeStrings = []string{
	"", "\x00", "a\x00b", "é", "世界", "😀", "\u007f\u0080߿ࠀ￿\U00010000\U0010ffff",
	"\x80", "\xbf", "\xc0\x80", "\xc1\xbf", "\xc2\x80", "\xf5\x80\x80\x80", "\xf8", "\xff",
	"\xe0\x9f\xbf", "\xe0\xa0\x80", "\xed\x9f\xbf", "\xed\xa0\x80", "\xef\xbf\xbf",
	"\xf0\x8f\xbf\xbf", "\xf0\x90\x80\x80", "\xf4\x8f\xbf\xbf", "\xf4\x90\x80\x80",
	"\xc3", "\xe4\xb8", "\xf0\x9f\x98", "\xe4\xb8a", "\xf0\x9f\x98\xe4\xb8\x96",
}

// runeValues are the int64 values TestRuneConversions encodes: the
// boundaries of each length, the surrogates, and values that are not code
// points.
var runeValues = []int64{0, 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000,
	0x10ffff, 0x110000, -1, -0x80000000, 1 << 32, 1<<32 + 'a', -1 << 63}

// TestRuneConversions checks []rune(s), string(rs), and string(x) of an
// integer against Go's own conversions, in the interpreter, the default
// backend, and -O0. Each string is also made at run time, at another
// address.
func TestRuneConversions(t *testing.T) {
	var src, want strings.Builder
	src.WriteString(`package main

func show(rs []rune) {
	print(len(rs), ":")
	for i := 0; i < len(rs); i++ {
		print(" ", rs[i])
	}
	println()
}

func bytes(s string) {
	print(len(s), ":")
	for i := 0; i < len(s); i++ {
		print(" ", s[i])
	}
	println()
}

func main() {
`)
	show := func(rs []rune) {
		fmt.Fprintf(&want, "%d:", len(rs))
		for _, r := range rs {
			fmt.Fprintf(&want, " %d", r)
		}
		want.WriteString("\n")
	}
	bytes := func(s string) {
		fmt.Fprintf(&want, "%d:", len(s))
		for i := 0; i < len(s); i++ {
			fmt.Fprintf(&want, " %d", s[i])
		}
		want.WriteString("\n")
	}
	for _, s := range runeStrings {
		q := strconv.Quote(s)
		fmt.Fprintf(&src, "\tshow([]rune(%s))\n\tshow([]rune(string([]byte(%s))))\n\tbytes(string([]rune(%s)))\n", q, q, q)
		show([]rune(s))
		show([]rune(s))
		bytes(string([]rune(s)))
	}
	for i, v := range runeValues {
		fmt.Fprintf(&src, "\tx%d := int64(%d)\n\tbytes(string(x%[1]d))\n\tbytes(string(rune(x%[1]d)))\n\tbytes(string([]rune{rune(x%[1]d), 'a'}))\n", i, v)
		// string(x) of an int64 is the encoding of its value, which is
		// not a code point outside the rune range.
		if v == int64(rune(v)) {
			bytes(string(rune(v)))
		} else {
			bytes("\uFFFD")
		}
		bytes(string(rune(v)))
		bytes(string([]rune{rune(v), 'a'}))
	}
	src.WriteString("}\n")
	w := result{stderr: want.String()}
	got, ds, err := interpret("runes.go", []byte(src.String()))
	if len(ds) > 0 || err != nil || got != w {
		t.Fatalf("interpreter: %v %v\n%s\nwant:\n%s", ds, err, got.stderr, w.stderr)
	}
	requireTools(t)
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(t.TempDir(), "runes")
		if ds, err := build([]source{{"runes.go", []byte(src.String())}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("build -O0=%v: %v %v", o0, ds, err)
		}
		if got := runExe(t, exe); got != w {
			t.Errorf("native -O0=%v: %+v", o0, got)
		}
	}
}

// TestStringOrder checks < <= > >= of every pair of streqStrings, the
// second also made at run time, against Go's own ordering, in the
// interpreter, the default backend, and -O0.
func TestStringOrder(t *testing.T) {
	var src, want strings.Builder
	src.WriteString("package main\n\nfunc mk(s string) string { return string([]byte(s)) }\n\nfunc main() {\n")
	for _, x := range streqStrings {
		for _, y := range streqStrings {
			qx, qy := strconv.Quote(x), strconv.Quote(y)
			fmt.Fprintf(&src, "\tprintln(%s < %s, mk(%s) <= %s, %s > mk(%s), mk(%s) >= mk(%s))\n", qx, qy, qx, qy, qx, qy, qx, qy)
			fmt.Fprintf(&want, "%t %t %t %t\n", x < y, x <= y, x > y, x >= y)
		}
	}
	src.WriteString("}\n")
	w := result{stderr: want.String()}
	got, ds, err := interpret("order.go", []byte(src.String()))
	if len(ds) > 0 || err != nil || got != w {
		t.Fatalf("interpreter: %v %v\n%+v", ds, err, got)
	}
	requireTools(t)
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(t.TempDir(), "order")
		if ds, err := build([]source{{"order.go", []byte(src.String())}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("build -O0=%v: %v %v", o0, ds, err)
		}
		if got := runExe(t, exe); got != w {
			t.Errorf("native -O0=%v: %+v", o0, got)
		}
	}
}

// TestGeneratedStringFuncs checks that lowering adds each generated string
// function only to a program that needs it, and once.
func TestGeneratedStringFuncs(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{`func main() { s := "a"; println(s + s, 'a', s == s) }`, []string{".streq"}},
		{`func main() { println("a" < "b", string(rune(65)), "é" >= "e") }`, nil}, // constants
		{`func main() { s := "a"; println(s < s, s >= "b") }`, []string{".strcmp"}},
		{`func main() { r := 'a'; println(string(r), string(r+1)) }`, []string{".encoderune"}},
		{`func main() { s := "a"; println(len([]rune(s)), len([]rune(s+s))) }`, []string{".stringtorunes", ".decoderune"}},
		{`func main() { r := []rune{1}; println(string(r), string(r[:0])) }`, []string{".runestostring", ".encoderune"}},
		{`func main() { r := 'a'; println(string(r), string([]rune{r})) }`, []string{".encoderune", ".runestostring"}},
		{`type aether struct{}; func (aether) streq() bool { return true }; func (*aether) strcmp() {}; func main() { s := "a"; println(s == s, aether{}.streq()) }`, []string{".streq"}}, // methods aether.streq and aether.strcmp
	}
	for _, tt := range tests {
		prog, ds := compile([]source{{"p.go", []byte("package main\n" + tt.src + "\n")}})
		if len(ds) > 0 {
			t.Errorf("%s: %v", tt.src, ds)
			continue
		}
		var got []string
		for _, f := range prog.Funcs {
			if strings.HasPrefix(f.Name, ".") {
				got = append(got, f.Name)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("%s: generated %v, want %v", tt.src, got, tt.want)
		}
	}
}
