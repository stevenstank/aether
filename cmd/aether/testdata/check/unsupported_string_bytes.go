// aether:expect unsupported conversion

package main

func main() {
	s := "xyz"
	b := []byte(s)
	println(string(s[0]), len(b))
}
