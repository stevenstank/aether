package main

import "strconv"

func main() {
	println(strconv.Itoa(-1234), strconv.FormatInt(255, 16), strconv.FormatUint(1<<63, 2), strconv.Quote("a\tb\"ü\x00"))
	n, err := strconv.Atoi("  12")
	println(n, err.Error())
	m, err := strconv.ParseInt("-0x1f", 0, 64)
	println(m, err == nil)
	_, err = strconv.ParseInt("99999999999999999999", 10, 64)
	println(err.Error())
	b, _ := strconv.ParseBool("true")
	println(b, strconv.FormatBool(false), strconv.QuoteRune('☺'), strconv.QuoteToASCII("日本"))
	for _, f := range []float64{0, 1, -2.5, 1e21, 1.0 / 3, 123456789.125, 5e-324} {
		println(strconv.FormatFloat(f, 'g', -1, 64), strconv.FormatFloat(f, 'e', 3, 64), strconv.FormatFloat(f, 'f', 2, 32))
	}
	x, err := strconv.ParseFloat("3.14159e2", 64)
	println(x, err == nil)
	s, err := strconv.Unquote(`"é\n"`)
	println(s, err == nil, string(strconv.AppendInt([]byte("n="), 42, 10)))
}
