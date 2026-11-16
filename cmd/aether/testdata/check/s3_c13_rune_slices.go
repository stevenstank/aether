// aether:expect run

package main

// []rune(s) decodes UTF-8, each invalid byte as U+FFFD, and string(rs)
// encodes each rune, any that is not a code point as U+FFFD.

func runes(s string) {
	rs := []rune(s)
	print(len(rs), ":")
	for i := 0; i < len(rs); i++ {
		print(" ", rs[i])
	}
	println()
	back := string(rs)
	print(len(back), ":")
	for i := 0; i < len(back); i++ {
		print(" ", back[i])
	}
	println(" ", back == s)
}

type word struct{ w []rune }

func main() {
	inputs := []string{
		"", "a", "\x00", "a\x00b", "héllo", "世界", "😀", "a😀b", "߿ࠀ￿\U00010000\U0010ffff",
		// Invalid: a lone continuation byte, bytes never valid, overlong
		// forms, surrogates, above U+10FFFF, and sequences cut short, at
		// the end and before another byte.
		"\x80", "\xbf", "\xc0\x80", "\xc1\xbf", "\xf5\x80\x80\x80", "\xff", "\xfe",
		"\xe0\x80\x80", "\xe0\x9f\xbf", "\xe0\xa0\x80", "\xed\x9f\xbf", "\xed\xa0\x80", "\xed\xbf\xbf", "\xee\x80\x80",
		"\xf0\x8f\xbf\xbf", "\xf0\x90\x80\x80", "\xf4\x8f\xbf\xbf", "\xf4\x90\x80\x80",
		"\xc3", "\xe4\xb8", "\xf0\x9f\x98", "\xc3a", "\xe4\xb8a", "\xf0\x9f\x98a", "a\xf0\x9f\x98\x80\x80",
		"\xc3\xc3\xa9", "\xe4\xb8\xe4\xb8\x96",
	}
	for i := 0; i < len(inputs); i++ {
		runes(inputs[i])
	}

	// An empty or nil []rune makes the empty string; []rune of the empty
	// string is empty but not nil.
	var nilRunes []rune
	println(string(nilRunes) == "", len(string([]rune{})), []rune("") == nil, len([]rune("")), nilRunes == nil)

	// Runes that are not code points.
	bad := []rune{-1, 0xd800, 0xdfff, 0x110000, 'x', -0x80000000}
	s := string(bad)
	println(len(s), s)

	// The result is a copy: changing the runes afterwards changes
	// neither.
	rs := []rune("abc")
	s = string(rs)
	rs[0] = '世'
	t := string(rs[1:])
	rs = append(rs, '!')
	println(s, string(rs), t, len(rs))

	// A field, and []int32.
	w := word{[]rune("ünï")}
	w.w[1] = 'N'
	var i32 []int32 = []int32("é")
	println(string(w.w), len(w.w), i32[0], string(i32))

	// The runes of a string made at run time, appended to.
	b := []byte("x")
	b = append(b, 0xe2, 0x82)
	println(len([]rune(string(b))), string(append(b, 0xac)), len([]rune(string(append(b, 0xac)))))
}
