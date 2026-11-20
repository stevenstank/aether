package main

import "unicode"

func main() {
	for _, r := range "aZ9 _é世 !Σ" {
		println(r, unicode.IsLetter(r), unicode.IsUpper(r), unicode.IsLower(r), unicode.IsDigit(r), unicode.IsSpace(r), unicode.IsPunct(r))
		println(unicode.ToUpper(r), unicode.ToLower(r), unicode.ToTitle(r), unicode.IsPrint(r), unicode.IsGraphic(r))
	}
	println(unicode.Is(unicode.Han, '世'), unicode.In('x', unicode.Latin, unicode.Greek), unicode.SimpleFold('k'))
}
