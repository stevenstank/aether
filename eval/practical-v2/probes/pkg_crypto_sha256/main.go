package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func main() {
	s := sha256.Sum256([]byte("abc"))
	fmt.Println(hex.EncodeToString(s[:]))
}
