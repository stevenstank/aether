// aether:expect run

package main

// switch on strings: the tag is evaluated once, then the case values in
// order, each compared by its bytes, until one matches; a default clause
// anywhere runs only when none does.

func v(s string) string {
	println("value", s)
	return s
}

func classify(s string) int {
	switch s {
	case "":
		return 0
	case "a", "b":
		return 1
	default:
		return 9
	case "x\x00", "\xff":
		return 2
	case "longer string":
		return 3
	}
}

func main() {
	words := []string{"", "a", "b", "c", "x\x00", "x", "\xff", "longer string", "longer strin"}
	for i := 0; i < len(words); i++ {
		println(i, classify(words[i]), classify(string([]byte(words[i]))))
	}
	switch v("t" + "ag") {
	case v("x"), v("tag"), v("never"):
		println("matched")
	case v("tag"):
		println("not reached")
	}
	switch s := v("q"); s + s {
	case v("q"):
	default:
		println("default")
	case v("qq"):
		println("qq")
		break
		println("not reached")
	}
	switch "" {
	case v("a"):
	}
	println("end")
}
