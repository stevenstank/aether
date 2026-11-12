// aether:expect unsupported string-comparison

package main

func main() {
	s := "x"
	println(s == "x", s < "y")
	switch s {
	case "x":
	}
}
