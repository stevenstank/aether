// aether:expect unsupported composite-literal

package main

func main() {
	println(len([]struct{ error }{{}}))
}
