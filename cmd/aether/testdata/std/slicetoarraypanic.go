package main

func main() {
	s := make([]int, 2, 8)
	p := (*[3]int)(s)
	println(p[0])
}
