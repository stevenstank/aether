// aether:expect run

package main

// String constants, variables, len, and printing: the empty string, ASCII,
// multibyte UTF-8, embedded NUL bytes, invalid UTF-8, raw strings, and every
// byte value an escape can write. Lengths count bytes, not code points.

const greeting = "héllo, 世界"
const typed string = "typed"
const n = len(greeting)

func main() {
	var empty string
	e := ""
	ascii := "The quick brown fox"
	utf8 := "日本語 ✓ 🙂"
	nul := "a\x00b\x00"
	bad := "\xff\xfe\xc3\x28 \xe2\x82"
	raw := `raw \n "quoted" \x00`
	esc := "\a\b\f\n\r\t\v\\\"\101é\U0001F600"
	println(empty, len(empty), e, len(e), empty, e)
	println(ascii, len(ascii))
	println(utf8, len(utf8))
	println(nul, len(nul))
	println(bad, len(bad))
	println(raw, len(raw))
	println(esc, len(esc))
	println(greeting, typed, n, len(greeting), len(typed), len("日本"))
	print(ascii, empty, utf8, 1, true, 2.5, nul, "\n")
	print()
	print("", "")
	println("")
	println("", "", "")
	println()
	// The same constant twice, and a substring of another.
	same := "héllo, 世界"
	sub := "héllo"
	println(same, sub, len(same) == len(greeting))
	// Assignments copy the two words.
	a, b := ascii, empty
	a, b = b, a
	println(a, len(a), b, len(b))
	empty = utf8
	utf8 = ""
	println(empty, utf8, len(empty), len(utf8))
}
