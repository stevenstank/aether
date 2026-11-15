// aether:expect run

package main

// String concatenation, + and +=, of constants, variables, globals, fields,
// elements, call results, and empty strings; operands are evaluated left to
// right, and a concatenation never changes its operands.

var g = "g"

type pair struct {
	a, b string
}

func tag(s string) string {
	println("tag", s)
	return "<" + s + ">"
}

func twice(s string) (string, string) { return s + s, s }

func main() {
	s := "abc"
	t := s + "-" + s
	println(t, len(t), s)
	println(s+"", ""+s, len(""+""), len(s+s+s+s))
	s += "d"
	s += s
	println(s, len(s))
	println(tag("x") + tag("y") + (tag("z") + tag("w")))
	g += g + "h"
	println(g)
	var arr [3]string
	arr[2] += "e"
	arr[2] += arr[2]
	arr[0] = arr[2] + arr[1] + g
	println(arr[0], arr[1], arr[2])
	p := &pair{a: "p"}
	p.b += p.a + "q"
	p.a = p.a + p.b
	println(p.a, p.b)
	u, v := twice("m")
	println(u+v, len(u+v))
	w := ""
	for i := 0; i < 5; i++ {
		w += t[i:i+1] + w
	}
	println(w, len(w))
	var all string
	for i := 0; i < 256; i++ {
		all += string([]byte{byte(i)})
	}
	println(len(all), all[0], all[255], all[128])
	k := all + all
	println(len(k), k[256], k[511])
}
