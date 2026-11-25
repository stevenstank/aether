// rand prints math/rand's results (D-053): a seeded generator's, which are
// the same everywhere, and the top-level functions', which are only with
// GODEBUG=randautoseed=0 (the global source seeded with 1) or
// randseednop=0 (Seed seeds it); otherwise only their ranges.
package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
)

func main() {
	r := rand.New(rand.NewSource(-12345))
	fmt.Println(r.Int(), r.Intn(10), r.Int31n(7), r.Int63n(1e15), r.Uint32(), r.Uint64(), r.Float32(), r.Float64())
	fmt.Println(r.NormFloat64(), r.ExpFloat64(), r.Perm(10))
	s := []int{1, 2, 3, 4, 5, 6, 7}
	r.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
	b := make([]byte, 13)
	r.Read(b)
	z := rand.NewZipf(r, 1.5, 2, 1000)
	fmt.Println(s, b, z.Uint64(), z.Uint64())
	var sum float64
	for range 50000 {
		sum += r.NormFloat64()*3 + r.ExpFloat64()
	}
	fmt.Println(sum)
	d := os.Getenv("GODEBUG")
	rand.Seed(99)
	vals := []any{rand.Intn(1000), rand.Int63(), rand.Float64(), rand.NormFloat64(), rand.Perm(5)}
	if strings.Contains(d, "randautoseed=0") || strings.Contains(d, "randseednop=0") {
		fmt.Println(vals...)
	} else {
		fmt.Println(vals[0].(int) < 1000, vals[1].(int64) >= 0, vals[2].(float64) < 1, len(vals[4].([]int)))
	}
}
