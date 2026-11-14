package main

// Field and element access in place: a matrix product over arrays of
// arrays, and an insertion sort over an array of structs, by index.

type Rec struct {
	key   int32
	value int64
	flag  bool
}

func main() {
	var a, b, c [48][48]int32
	for i := 0; i < 48; i++ {
		for j := 0; j < 48; j++ {
			a[i][j] = int32(i + j)
			b[i][j] = int32(i - j)
		}
	}
	for r := 0; r < 400; r++ {
		for i := 0; i < 48; i++ {
			for j := 0; j < 48; j++ {
				var s int32
				for k := 0; k < 48; k++ {
					s += a[i][k] * b[k][j]
				}
				c[i][j] = s + int32(r)
			}
		}
		a[r%48][r%48] = c[r%48][47-r%48]
	}
	var recs [600]Rec
	seed := int32(12345)
	for round := 0; round < 120; round++ {
		for i := 0; i < 600; i++ {
			seed = seed*1103515245 + 12345
			recs[i].key = seed >> 16
			recs[i].value = int64(i)
			recs[i].flag = seed&1 == 0
		}
		for i := 1; i < 600; i++ {
			k := recs[i].key
			v := recs[i].value
			f := recs[i].flag
			j := i - 1
			for j >= 0 && recs[j].key > k {
				recs[j+1].key = recs[j].key
				recs[j+1].value = recs[j].value
				recs[j+1].flag = recs[j].flag
				j--
			}
			recs[j+1].key = k
			recs[j+1].value = v
			recs[j+1].flag = f
		}
	}
	println(c[0][0], c[47][47], c[23][11], recs[0].key, recs[599].key, recs[300].value, recs[300].flag)
}
