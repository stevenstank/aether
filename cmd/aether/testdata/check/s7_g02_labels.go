// aether:expect run

package main

// Labels, labeled break and continue of for, range, switch, select, and
// type switch statements, and goto forward and backward (D-049).

func find(m [][]int, x int) (int, int) {
outer:
	for i, row := range m {
		for j, v := range row {
			if v == x {
				return i, j
			}
			if v > x {
				continue outer
			}
		}
	}
	return -1, -1
}

func collatz(n int) (steps int) {
loop:
	if n == 1 {
		goto done
	}
	steps++
	if n%2 == 0 {
		n /= 2
	} else {
		n = 3*n + 1
	}
	goto loop
done:
	return
}

func classify(xs []any) (ints, strs int) {
L:
	for _, x := range xs {
		switch x.(type) {
		case int:
			ints++
		case string:
			strs++
		case nil:
			break L
		default:
			continue L
		}
	}
	return
}

func main() {
	m := [][]int{{1, 5, 2}, {3, 4}, {9, 7}}
	println(find(m, 4))
	println(find(m, 2))
	println(find(m, 8))
	println(collatz(27))
	println(classify([]any{1, "a", 2.5, 3, nil, "b"}))

	n := 0
sw:
	switch {
	case n == 0:
		for {
			n++
			if n > 3 {
				break sw
			}
		}
	}
	println("n", n)

	ch := make(chan int, 1)
	count := 0
sel:
	for {
		select {
		case ch <- count:
		case v := <-ch:
			count += v + 1
			if count > 10 {
				break sel
			}
		}
	}
	println("count", count)

	i := 0
back:
	i++
	if i < 5 {
		goto back
	}
	println("i", i)

	goto skip
	println("not printed")
skip:
	for k := range 3 {
		if k == 1 {
			goto end
		}
		println("k", k)
	}
end:
	println("end")
}
