// aether:expect run

package main

// < <= > >= of strings: lexicographic by bytes, read as unsigned, with a
// proper prefix first; both operands are evaluated, left to right, before
// they are compared.

func v(s string) string {
	println("value", s)
	return s
}

func mk(s string) string { return string([]byte(s)) }

func main() {
	words := [12]string{"", "\x00", "\x00\x00", "a", "a\x00", "ab", "abc", "abd", "b", "\x7f", "\x80", "\xff"}
	for i := 0; i < len(words); i++ {
		for j := 0; j < len(words); j++ {
			x, y := words[i], mk(words[j])
			print(x < y, " ", x <= y, " ", x > y, " ", x >= y, " | ")
		}
		println()
	}

	// Equal bytes at different addresses, and the empty string of any
	// origin.
	s := "héllo"
	t := mk(s)
	println(s < t, s <= t, s > t, s >= t, s[:0] < t[5:], "" >= s[1:1])
	println("é" < "é", "z" < "é", "\U0001F600" > "￿", "Z" < "a")

	// Evaluation order.
	println(v("b") < v("a"), v("x") >= v("x"))

	// A sort of strings made at run time.
	var a [8]string
	src := []string{"pear", "apple", "", "fig", "app", "Apple", "apple\x00", "\xffend"}
	for i := 0; i < len(src); i++ {
		a[i] = mk(src[i])
	}
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
	for i := 0; i < len(a); i++ {
		println(i, a[i], len(a[i]))
	}

	// Constants, folded, and in conditions.
	const c = "abc" < "abd"
	if s > "h" && s <= "i" {
		println("between", c)
	}
	for k := "a"; k < "aaaa"; k += "a" {
		print(k, " ")
	}
	println()
}
