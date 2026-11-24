package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

type header struct {
	Magic   [4]byte
	Version uint16
	Flags   int16
	Size    uint32
	Offset  int64
	Ratio   float64
	Ok      bool
	_       [3]byte
	Small   int8
}

func main() {
	// Varints at the edges.
	for _, u := range []uint64{0, 1, 127, 128, 300, 16383, 16384, 1<<32 - 1, 1<<63 - 1, math.MaxUint64} {
		b := binary.AppendUvarint(nil, u)
		v, n := binary.Uvarint(b)
		r, err := binary.ReadUvarint(bytes.NewReader(b))
		fmt.Printf("%d % x %d %d %d %v\n", u, b, v, n, r, err)
	}
	for _, i := range []int64{0, -1, 1, -64, 64, math.MinInt64, math.MaxInt64} {
		var buf [binary.MaxVarintLen64]byte
		n := binary.PutVarint(buf[:], i)
		v, m := binary.Varint(buf[:n])
		fmt.Printf("%d % x %d %d\n", i, buf[:n], v, m)
	}
	// Truncated and overflowing input.
	fmt.Println(binary.Uvarint([]byte{0x80, 0x80}))
	fmt.Println(binary.Uvarint(bytes.Repeat([]byte{0xff}, 11)))
	_, err := binary.ReadUvarint(bytes.NewReader([]byte{0x80}))
	fmt.Println(err, errors.Is(err, io.ErrUnexpectedEOF))
	_, err = binary.ReadUvarint(bytes.NewReader(nil))
	fmt.Println(err == io.EOF)
	_, err = binary.ReadUvarint(bytes.NewReader(bytes.Repeat([]byte{0xff}, 11)))
	fmt.Println(err)

	// Byte orders.
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, 0x0102030405060708)
	fmt.Printf("% x %x %x\n", b, binary.BigEndian.Uint64(b), binary.LittleEndian.Uint32(b[2:]))
	binary.BigEndian.PutUint16(b, 0xBEEF)
	fmt.Printf("% x %v %v\n", b[:2], binary.BigEndian, binary.LittleEndian)
	out := binary.BigEndian.AppendUint32(nil, 7)
	out = binary.LittleEndian.AppendUint16(out, 0x1234)
	out = binary.NativeEndian.AppendUint64(out, 1)
	fmt.Printf("% x\n", out)

	// Fixed-size data through reflection, and the fast paths.
	h := header{Magic: [4]byte{'A', 'E', 'T', 'H'}, Version: 3, Flags: -2, Size: 1 << 20, Offset: -12345678901, Ratio: 0.75, Ok: true, Small: -7}
	var w bytes.Buffer
	if err := binary.Write(&w, binary.BigEndian, h); err != nil {
		fmt.Println("write:", err)
	}
	fmt.Printf("%d % x\n", binary.Size(h), w.Bytes())
	var back header
	if err := binary.Read(bytes.NewReader(w.Bytes()), binary.BigEndian, &back); err != nil {
		fmt.Println("read:", err)
	}
	fmt.Printf("%+v %v\n", back, back == h)
	w.Reset()
	binary.Write(&w, binary.LittleEndian, []uint32{1, 2, 0xdeadbeef})
	binary.Write(&w, binary.LittleEndian, int16(-1))
	binary.Write(&w, binary.LittleEndian, []float32{1.5})
	fmt.Printf("% x\n", w.Bytes())
	nums := make([]uint32, 3)
	fmt.Println(binary.Read(bytes.NewReader(w.Bytes()), binary.LittleEndian, nums), nums)
	enc, err := binary.Append(nil, binary.BigEndian, struct{ A, B uint16 }{1, 2})
	fmt.Printf("% x %v\n", enc, err)
	n, err := binary.Encode(make([]byte, 2), binary.BigEndian, uint32(1))
	fmt.Println(n, err)
	fmt.Println(binary.Write(&w, binary.BigEndian, "not fixed size"))
	fmt.Println(binary.Read(bytes.NewReader([]byte{1}), binary.BigEndian, &back))
	fmt.Println(binary.Size([]int{1}), binary.Size(struct{ S []byte }{}))
}
