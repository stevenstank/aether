// aether:expect run

package main

// == and != of strings compare their bytes, not their addresses: equal
// bytes made at different places are equal, empty strings are equal
// whatever their origin, and NUL bytes, invalid UTF-8, and prefixes are
// compared byte by byte, not as Unicode.

var g = "global"

func mk(s string) string { return s + "" }

func id(s string) string {
	println("id", s)
	return s
}

func main() {
	a := "hello"
	b := mk("hel") + mk("lo")
	c := string([]byte(a))
	println(a == b, a == c, b == c, a != b, a == a)
	var e string
	empty := []string{"", e, a[5:], a[:0], b[2:2], string([]byte(nil)), mk("")}
	for i := 0; i < len(empty); i++ {
		println(i, empty[i] == "", empty[i] == empty[(i+1)%len(empty)], empty[i] != a)
	}
	println(a == "hell", a == "hello!", "hell" == a, a[:4] == "hell", a[1:] == "ello")
	println("x\x00y" == "x\x00z", "x\x00y" == mk("x\x00y"), "x\x00" == "x", "\x00" == "")
	println("\xff\xfe" == mk("\xff\xfe"), "\xff" == "\xfe", "\x80" == "\xc0\x80")
	println("é" == "é", "é" == "\xc3\xa9", "日本" == mk("日")+"本")
	println(g == "glob"+"al", g != mk(g), g[:4] == "glob")
	p := &g
	q := new(string)
	*q = mk("global")
	println(*p == *q, p == q)
	println(id("a") == id("b"), id("c") != id("c"))
	var all []byte
	for i := 0; i < 256; i++ {
		all = append(all, byte(i))
	}
	s1, s2 := string(all), string(all)
	println(s1 == s2, s1[1:] == s2[:255], s1[255:] == "\xff")
	all[200] = 0
	println(string(all) == s1, string(all[:200]) == s1[:200])
}
