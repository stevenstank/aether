// aether:expect unsupported composite-literal

package main

func main() {
	println(len([][1 << 22]int{{}}))
}
