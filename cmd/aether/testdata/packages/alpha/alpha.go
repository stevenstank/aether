package alpha

import "example.com/pk/base"

type Kind int

func (k Kind) String() string { return "alpha kind" }

var count = base.Count * 2

func init() {
	base.Note("alpha.init")
	count++
}

func Count() int { return count }

func sum[T int | float64](xs []T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func Total(xs []int) int       { return sum(xs) }
func Mean(xs []float64) float64 { return sum(xs) / float64(len(xs)) }

// Counter returns a closure over a new variable.
func Counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}

// Gen sends 0 to n-1 on a new channel from a goroutine.
func Gen(n int) chan int {
	c := make(chan int)
	go func() {
		for i := range n {
			c <- i
		}
		close(c)
	}()
	return c
}
