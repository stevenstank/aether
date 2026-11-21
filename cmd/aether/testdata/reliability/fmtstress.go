// fmt and strconv, compiled from GOROOT source, over many verbs, flags,
// widths, and types: the formatting state machine, reflection over
// composite values, sorted map printing, the Stringer, GoStringer,
// Formatter, and error interfaces, bad verbs, and argument errors.
package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type color int

const (
	red color = iota
	green
	blue
)

func (c color) String() string { return [...]string{"red", "green", "blue"}[c] }

type vec struct {
	X, Y float64
	tag  string
}

func (v vec) GoString() string { return fmt.Sprintf("vec(%g,%g)", v.X, v.Y) }

var errMissing = errors.New("missing")

type money int64

func (m money) Format(f fmt.State, verb rune) {
	w, wok := f.Width()
	p, pok := f.Precision()
	fmt.Fprintf(f, "$%d.%02d[%c w=%d/%v p=%d/%v plus=%v minus=%v]", int64(m)/100, int64(m)%100, verb, w, wok, p, pok, f.Flag('+'), f.Flag('-'))
}

type tree struct {
	Val   int
	Left  *tree
	Right *tree
	Meta  map[string]any
	Kids  []tree
	Color color
	Err   error
}

type key struct {
	A int
	B string
}

func main() {
	ints := []any{0, -1, 42, int8(-128), uint8(255), int64(math.MinInt64), uint64(math.MaxUint64), uintptr(7)}
	for _, v := range ints {
		fmt.Printf("%v|%d|%5d|%-5d|%05d|%x|%X|%#x|%o|%#o|%O|%b|%+d|% d|%c|%q|%U|%T\n", v, v, v, v, v, v, v, v, v, v, v, v, v, v, v, v, v, v)
	}
	floats := []float64{0, -0.0, 1, -1.5, 3.14159265358979, 1e-7, 123456789.125, 1e21, 1e100, math.Inf(1), math.Inf(-1), math.NaN(), math.SmallestNonzeroFloat64, math.MaxFloat64}
	for _, f := range floats {
		fmt.Printf("%v|%g|%.3g|%e|%.2E|%f|%.0f|%8.3f|%-10.4f|%+.1f|%x|%G|%#g\n", f, f, f, f, f, f, f, f, f, f, f, f, f)
	}
	fmt.Printf("%v %.2f %g\n", float32(0.1), float32(1)/3, float32(16777216.0))
	strs := []string{"", "hello", "tab\there", "quote\"s", "üñí©ødé", "\x00\xff", "日本語"}
	for _, s := range strs {
		fmt.Printf("%s|%q|%+q|%#q|%x|% x|%X|%10s|%-10s|%.3s|%5.2q|%v\n", s, s, s, s, s, s, s, s, s, s, s, s)
	}
	fmt.Printf("%v %s %d %x\n", []byte("hi"), []byte("hi"), []byte("hi"), []byte("hi"))
	fmt.Printf("%t %v %5t %q %c %U %#U\n", true, false, true, 'x', 0x1F600, 0x1F600, 'ä')
	fmt.Printf("%v %d %s %x %q\n", red, blue, green, green, blue)
	v := vec{1.5, -2, "t"}
	fmt.Printf("%v|%+v|%#v|%T\n", v, v, v, v)
	fmt.Printf("%v|%+v|%#v\n", &v.X == nil, []vec{v, {}}, [2]bool{true})
	m := money(12345)
	fmt.Printf("%v|%+8.3d|%-s|%x\n", m, m, m, m)
	fmt.Println(fmt.Sprint(m), fmt.Sprintf("%10v", m))
	tr := tree{Val: 1, Left: &tree{Val: 2}, Meta: map[string]any{"z": 1, "a": []int{1, 2}, "m": map[int]string{3: "c", 1: "a", 2: "b"}}, Kids: []tree{{Val: 3, Color: blue}}, Color: green, Err: errors.New("bad")}
	s := fmt.Sprintf("%v", tr)
	fmt.Println(s[strings.Index(s, "map"):])
	s = fmt.Sprintf("%+v", tr.Kids)
	fmt.Println(s)
	fmt.Printf("%v %v %v\n", map[key]int{{2, "b"}: 1, {1, "z"}: 2, {1, "a"}: 3}, map[bool]int{true: 1, false: 0}, map[float64]string{2.5: "x", -1: "y", math.Inf(1): "inf"})
	fmt.Printf("%v %+v %#v %v %v\n", []int(nil), map[string]int(nil), []string{}, (*int)(nil), error(nil))
	fmt.Printf("%d %s\n", "str", 5)
	fmt.Printf("%d %d\n", 1)
	fmt.Printf("%d\n", 1, 2)
	fmt.Printf("%!|%z|%-+ #0d\n", 1, 2)
	fmt.Printf("%*d|%-*d|%.*f|%[2]d %[1]d|%[3]*.[2]*[1]f\n", 5, 42, 4, 7, 2, 3.14159, 12.0, 2, 6)
	fmt.Printf("%v %v\n", struct{}{}, struct {
		a int
		b []string
	}{1, []string{"x"}})
	var ip *int
	n := 5
	fmt.Printf("%v %v\n", ip == nil, *(&n))
	e1 := fmt.Errorf("layer1: %w", errMissing)
	e2 := fmt.Errorf("layer2: %w + %w", e1, errors.New("other"))
	fmt.Println(e2, errors.Is(e2, errMissing), errors.Unwrap(e1) == errMissing)
	fmt.Println(fmt.Sprintln("a", 1, 2, "b", "c", 3.0, nil), fmt.Sprint("a", 1, 2, "b", "c", 3.0, nil))
	fmt.Fprint(os.Stdout, "x", 1, "\n")

	for _, f := range []float64{0.1, 2.5, 1e23, 5e-324, 123.456, -0.000123} {
		for _, fmtc := range []byte{'e', 'E', 'f', 'g', 'G', 'b', 'x'} {
			for _, prec := range []int{-1, 0, 3, 17} {
				fmt.Print(strconv.FormatFloat(f, fmtc, prec, 64), " ")
			}
		}
		fmt.Println(strconv.FormatFloat(f, 'g', -1, 32))
	}
	for _, in := range []string{"1", "-0", "1e10", "1.7976931348623157e308", "1.8e308", "4.9e-324", "2e-324", "0x1p-2", "inf", "-Inf", "nan", "1_000.5", ".5", "5.", "abc", "", "1e", "0x10"} {
		f, err := strconv.ParseFloat(in, 64)
		fmt.Printf("%q:%v:%v ", in, f, err)
	}
	fmt.Println()
	for _, in := range []string{"0", "-9223372036854775808", "9223372036854775808", "0x7f", "0b101", "0o17", "017", "1_000", "-", "+5", "z"} {
		i, err := strconv.ParseInt(in, 0, 64)
		u, uerr := strconv.ParseUint(in, 0, 8)
		fmt.Printf("%q:%d:%v:%d:%v\n", in, i, err, u, uerr)
	}
	for _, base := range []int{2, 8, 10, 16, 36} {
		fmt.Print(strconv.FormatInt(-123456789, base), " ", strconv.FormatUint(math.MaxUint64, base), " ")
	}
	fmt.Println()
	fmt.Println(strconv.Quote("héllo\n\x01 "), strconv.QuoteToASCII("日本\t"), strconv.QuoteRune('☺'), strconv.Itoa(-0))
	uq, err := strconv.Unquote(`"a\tbé\x41"`)
	fmt.Println(uq, err)
	_, err = strconv.Unquote(`"bad`)
	fmt.Println(err)
	b := strconv.AppendInt([]byte("n="), -42, 10)
	b = strconv.AppendBool(append(b, ' '), true)
	b = strconv.AppendQuote(append(b, ' '), "q")
	b = strconv.AppendFloat(append(b, ' '), 1.0/3, 'f', 4, 64)
	fmt.Println(string(b))
	for _, in := range []string{"true", "F", "1", "yes"} {
		v, err := strconv.ParseBool(in)
		fmt.Print(v, err, " ")
	}
	fmt.Println()
	var sb strings.Builder
	for i := range 200 {
		fmt.Fprintf(&sb, "%03d:%-4x|", i*i%1000, i)
	}
	out := sb.String()
	fmt.Println(len(out), out[:60], out[len(out)-30:])
	var ss []string
	for _, w := range strings.Fields("  the quick  brown fox jumps over the lazy dog ") {
		ss = append(ss, strings.ToUpper(w[:1])+w[1:])
	}
	fmt.Println(strings.Join(ss, "-"), strings.Count("cheese", "e"), strings.LastIndex("go gopher", "go"), strings.Replace("oink oink oink", "k", "ky", 2), strings.Title("x"))
	fmt.Println(strings.Map(func(r rune) rune {
		if r == 'a' {
			return -1
		}
		return r + 1
	}, "banana"), strings.EqualFold("Straße", "STRASSE"), strings.TrimFunc("123abc456", func(r rune) bool { return r < 'a' }), strings.SplitN("a,b,c,d", ",", 2), strings.SplitAfter("a,b,c", ","))
}
