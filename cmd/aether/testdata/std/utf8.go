package main

import "unicode/utf8"

func main() {
	s := "héllo, 世界\xff"
	println(utf8.RuneCountInString(s), utf8.ValidString(s), utf8.ValidString("ok"))
	r, n := utf8.DecodeRuneInString(s[1:])
	println(r, n, utf8.RuneLen('世'), utf8.FullRune([]byte{0xe4, 0xb8}))
	b := utf8.AppendRune(nil, '界')
	println(len(b), b[0], utf8.RuneStart(b[1]))
	r, n = utf8.DecodeLastRuneInString(s)
	println(r == utf8.RuneError, n)
}
