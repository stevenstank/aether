package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

func main() {
	b := binary.AppendUvarint(nil, 300)
	v, err := binary.ReadUvarint(bytes.NewReader(b))
	var s [4]byte
	binary.LittleEndian.PutUint32(s[:], 7)
	fmt.Println(v, err, s)
}
