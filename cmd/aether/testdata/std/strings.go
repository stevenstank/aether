package main

import (
	"bytes"
	"strings"
	"unicode"
)

func main() {
	s := "The quick brown fox jumps over the lazy dog; the end"
	println(strings.Index(s, "fox"), strings.Index(s, "cat"), strings.LastIndex(s, "the"), strings.IndexByte(s, 'q'), strings.IndexRune(s, 'z'))
	println(strings.Index(s, "jumps over the lazy dog; the end and more"), strings.Index(strings.Repeat("ab", 40)+"abc", "ab"+strings.Repeat("ab", 20)+"c"))
	println(strings.Count(s, "o"), strings.Count("five", ""), strings.Contains(s, "lazy"), strings.ContainsRune(s, 'Q'), strings.ContainsAny(s, "xyz"))
	println(strings.HasPrefix(s, "The"), strings.HasSuffix(s, "end"), strings.EqualFold("Go", "GO"), strings.Compare("a", "b"))
	println(strings.ToUpper(s[:9]), strings.ToLower("ÀB"), strings.Title("x"), strings.TrimSpace("\t x y \n"), strings.Trim("xxhixx", "x"))
	println(strings.TrimLeft("aab", "a"), strings.TrimPrefix("prefix", "pre"), strings.TrimSuffix("a.go", ".go"), strings.TrimFunc("123abc456", unicode.IsDigit))
	parts := strings.Split("a,b,,c", ",")
	println(len(parts), parts[2] == "", strings.Join(parts, "|"), len(strings.Fields("  a b   c ")), strings.SplitN("a:b:c", ":", 2)[1])
	before, after, found := strings.Cut("key=value", "=")
	println(before, after, found, strings.Replace("oink oink oink", "k", "ky", 2), strings.ReplaceAll("oink oink", "oink", "moo"))
	println(strings.Map(func(r rune) rune { return r + 1 }, "HAL"), strings.Repeat("-", 5), strings.Clone("clone"), strings.LastIndexByte("abcabc", 'b'))
	println(strings.IndexFunc("hello, 世界", func(r rune) bool { return r > 127 }), strings.IndexAny("golang", "ny"), strings.LastIndexAny("golang", "go"))

	var b strings.Builder
	for i := range 20 {
		b.WriteString("x")
		b.WriteByte(byte('0' + i%10))
		b.WriteRune('é')
	}
	b.Grow(100)
	str := b.String()
	println(b.Len(), len(str), str[:8], strings.Count(str, "é"))
	b.Reset()
	println(b.Len(), b.String() == "")

	r := strings.NewReplacer("a", "1", "b", "2")
	println(r.Replace("abcab"))

	// bytes
	x := []byte("hello, world")
	println(bytes.Index(x, []byte("world")), bytes.Equal(x[:5], []byte("hello")), bytes.Compare([]byte("a"), []byte("b")), bytes.Count(x, []byte("l")))
	println(string(bytes.ToUpper(x)), string(bytes.TrimSpace([]byte("  t  "))), bytes.HasPrefix(x, []byte("he")), bytes.IndexByte(x, 'w'), bytes.LastIndex(x, []byte("o")))
	println(string(bytes.Join([][]byte{[]byte("a"), []byte("b")}, []byte(", "))), len(bytes.Split(x, []byte(","))), string(bytes.Repeat([]byte("z"), 3)))
	var buf bytes.Buffer
	buf.WriteString("buf")
	buf.WriteByte('!')
	buf.Write([]byte(" more"))
	println(buf.String(), buf.Len())
	c, _ := buf.ReadByte()
	println(string(rune(c)), buf.String())
}
