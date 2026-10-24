// aether:expect run

package main

func main() {
	x := 1
	{
		x := x + 10
		{
			x := x * 2
			println(x)
		}
		x++
		println(x)
	}
	println(x)
	;
}
