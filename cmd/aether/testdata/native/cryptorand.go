// cryptorand checks crypto/rand (D-057), which needs the FIPS module's
// 32 MiB entropy buffer, a package-level array larger than a frame may
// hold, and getrandom(2), which the interpreter does not make.
package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
)

func main() {
	a, b := make([]byte, 64), make([]byte, 64)
	n, err := rand.Read(a)
	rand.Read(b)
	fmt.Println(n, err, !bytes.Equal(a, b), !bytes.Equal(a, make([]byte, 64)))
	x, err := rand.Int(rand.Reader, big.NewInt(1000))
	fmt.Println(x.Sign() >= 0 && x.Cmp(big.NewInt(1000)) < 0, err)
	p, err := rand.Prime(rand.Reader, 64)
	fmt.Println(p.BitLen(), p.ProbablyPrime(20), err)
	s := rand.Text()
	fmt.Println(len(s))
}
