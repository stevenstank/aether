package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

func main() {
	data := []byte(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 60))
	fmt.Printf("%x\n", md5.Sum(data))
	fmt.Printf("%x\n", sha1.Sum(data))
	fmt.Printf("%x\n", sha256.Sum256(data))
	fmt.Printf("%x\n", sha256.Sum224(nil))
	fmt.Printf("%x\n", sha512.Sum512(data[:1000]))
	h := sha256.New()
	for i := 0; i < len(data); i += 997 {
		h.Write(data[i:min(i+997, len(data))])
	}
	fmt.Println(hex.EncodeToString(h.Sum(nil)), h.Size(), h.BlockSize())
	m := hmac.New(sha256.New, []byte("key"))
	m.Write([]byte("message"))
	mac := m.Sum(nil)
	fmt.Println(hex.EncodeToString(mac), hmac.Equal(mac, mac), hmac.Equal(mac, mac[:3]))
	for _, p := range [][2]string{{"abc", "abc"}, {"abc", "abd"}, {"", ""}, {"a", "ab"}} {
		fmt.Print(subtle.ConstantTimeCompare([]byte(p[0]), []byte(p[1])), " ")
	}
	fmt.Println(subtle.ConstantTimeByteEq(3, 3), subtle.ConstantTimeByteEq(3, 4), subtle.ConstantTimeSelect(1, 5, 6),
		subtle.ConstantTimeSelect(0, 5, 6), subtle.ConstantTimeEq(-1, -1), subtle.ConstantTimeLessOrEq(2, 3), subtle.ConstantTimeLessOrEq(4, 3))
	dst := make([]byte, 3)
	subtle.ConstantTimeCopy(1, dst, []byte("xyz"))
	copied := string(dst)
	n := subtle.XORBytes(dst, dst, []byte{1, 2, 3})
	fmt.Println(copied, n, dst)
}
