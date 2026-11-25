package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

func main() {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(7))
	u := binary.AppendUvarint(nil, 300)
	v, _ := binary.ReadUvarint(bytes.NewReader(u))
	fmt.Println(b.Len(), v, binary.LittleEndian.Uint32([]byte{1, 0, 0, 0}))
}
