package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// Container format:
//
//	magic "LZH1" | uvarint original size | uint32 CRC-32 (IEEE, little endian)
//	blocks: 1 bit "last", 286 + 30 four-bit code lengths, symbols, end symbol
const magic = "LZH1"

const (
	windowSize = 1 << 15
	minMatch   = 3
	maxMatch   = 258
	hashBits   = 15
	endOfBlock = 256
	numLitLen  = 286
	numDist    = 30
	blockToks  = 1 << 14
)

var lengthBase = [29]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31,
	35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258}
var lengthExtra = [29]uint{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2,
	3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
var distBase = [30]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193,
	257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}
var distExtra = [30]uint{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6,
	7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}

type token struct {
	lit    byte
	length int // 0 for a literal
	dist   int
}

func codeFor(v int, base []int) int {
	i := len(base) - 1
	for base[i] > v {
		i--
	}
	return i
}

type Stats struct {
	Literals, Matches int
	MatchBytes        int
	Blocks            int
	LongestMatch      int
}

// tokenize runs LZ77 with hash chains; level bounds the chain length.
func tokenize(data []byte, level int, st *Stats) []token {
	chain := 4 << level
	head := make([]int32, 1<<hashBits)
	for i := range head {
		head[i] = -1
	}
	prev := make([]int32, len(data))
	hash := func(i int) uint32 {
		v := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16
		return (v * 2654435761) >> (32 - hashBits)
	}
	insert := func(i int) {
		if i+minMatch <= len(data) {
			h := hash(i)
			prev[i] = head[h]
			head[h] = int32(i)
		}
	}
	var toks []token
	for i := 0; i < len(data); {
		bestLen, bestDist := 0, 0
		if i+minMatch <= len(data) && level > 0 {
			limit := min(maxMatch, len(data)-i)
			cand := head[hash(i)]
			for n := 0; cand >= 0 && n < chain && i-int(cand) <= windowSize; n++ {
				c := int(cand)
				if data[c+bestLen] == data[i+bestLen] || bestLen == 0 {
					l := 0
					for l < limit && data[c+l] == data[i+l] {
						l++
					}
					if l > bestLen {
						bestLen, bestDist = l, i-c
						if l == limit {
							break
						}
					}
				}
				cand = prev[c]
			}
		}
		if bestLen >= minMatch {
			toks = append(toks, token{length: bestLen, dist: bestDist})
			st.Matches++
			st.MatchBytes += bestLen
			st.LongestMatch = max(st.LongestMatch, bestLen)
			for k := 0; k < bestLen; k++ {
				insert(i + k)
			}
			i += bestLen
		} else {
			toks = append(toks, token{lit: data[i]})
			st.Literals++
			insert(i)
			i++
		}
	}
	return toks
}

func Compress(data []byte, level int, st *Stats) []byte {
	var out bytes.Buffer
	out.WriteString(magic)
	out.Write(binary.AppendUvarint(nil, uint64(len(data))))
	binary.Write(&out, binary.LittleEndian, crc32.ChecksumIEEE(data))
	bw := &bitWriter{w: &out}
	toks := tokenize(data, level, st)
	for start := 0; start < len(toks) || start == 0; start += blockToks {
		end := min(start+blockToks, len(toks))
		writeBlock(bw, toks[start:end], end == len(toks))
		st.Blocks++
		if end == len(toks) {
			break
		}
	}
	bw.flush()
	return out.Bytes()
}

func writeBlock(bw *bitWriter, toks []token, last bool) {
	litFreq := make([]int, numLitLen)
	distFreq := make([]int, numDist)
	for _, t := range toks {
		if t.length == 0 {
			litFreq[t.lit]++
		} else {
			litFreq[257+codeFor(t.length, lengthBase[:])]++
			distFreq[codeFor(t.dist, distBase[:])]++
		}
	}
	litFreq[endOfBlock]++
	litLens, distLens := codeLengths(litFreq), codeLengths(distFreq)
	litCodes, distCodes := canonicalCodes(litLens), canonicalCodes(distLens)
	if last {
		bw.writeBits(1, 1)
	} else {
		bw.writeBits(0, 1)
	}
	for _, l := range litLens {
		bw.writeBits(uint32(l), 4)
	}
	for _, l := range distLens {
		bw.writeBits(uint32(l), 4)
	}
	for _, t := range toks {
		if t.length == 0 {
			bw.writeBits(litCodes[t.lit], uint(litLens[t.lit]))
			continue
		}
		lc := codeFor(t.length, lengthBase[:])
		bw.writeBits(litCodes[257+lc], uint(litLens[257+lc]))
		bw.writeBits(uint32(t.length-lengthBase[lc]), lengthExtra[lc])
		dc := codeFor(t.dist, distBase[:])
		bw.writeBits(distCodes[dc], uint(distLens[dc]))
		bw.writeBits(uint32(t.dist-distBase[dc]), distExtra[dc])
	}
	bw.writeBits(litCodes[endOfBlock], uint(litLens[endOfBlock]))
}

type FormatError struct {
	Offset int64
	Err    error
}

func (e *FormatError) Error() string { return fmt.Sprintf("at byte %d: %v", e.Offset, e.Err) }
func (e *FormatError) Unwrap() error { return e.Err }

type countingReader struct {
	r *bufio.Reader
	n int64
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

func Decompress(r io.Reader, maxSize uint64) ([]byte, error) {
	cr := &countingReader{r: bufio.NewReader(r)}
	fail := func(err error) ([]byte, error) { return nil, &FormatError{Offset: cr.n, Err: err} }
	var hdr [4]byte
	for i := range hdr {
		b, err := cr.ReadByte()
		if err != nil {
			return fail(io.ErrUnexpectedEOF)
		}
		hdr[i] = b
	}
	if string(hdr[:]) != magic {
		return fail(fmt.Errorf("bad magic %q", hdr[:]))
	}
	size, err := binary.ReadUvarint(cr)
	if err != nil {
		return fail(err)
	}
	if size > maxSize {
		return fail(fmt.Errorf("declared size %d exceeds limit %d", size, maxSize))
	}
	var sum [4]byte
	for i := range sum {
		if sum[i], err = cr.ReadByte(); err != nil {
			return fail(io.ErrUnexpectedEOF)
		}
	}
	want := binary.LittleEndian.Uint32(sum[:])
	out := make([]byte, 0, size)
	br := &bitReader{r: cr}
	for {
		last, err := br.readBits(1)
		if err != nil {
			return fail(err)
		}
		lens := make([]uint8, numLitLen+numDist)
		for i := range lens {
			v, err := br.readBits(4)
			if err != nil {
				return fail(err)
			}
			lens[i] = uint8(v)
		}
		lit, err := newDecoder(lens[:numLitLen])
		if err != nil {
			return fail(err)
		}
		dist, err := newDecoder(lens[numLitLen:])
		if err != nil {
			return fail(err)
		}
		for {
			sym, err := lit.decode(br)
			if err != nil {
				return fail(err)
			}
			if sym < 256 {
				out = append(out, byte(sym))
			} else if sym == endOfBlock {
				break
			} else {
				lc := sym - 257
				if lc >= len(lengthBase) {
					return fail(ErrCorrupt)
				}
				extra, err := br.readBits(lengthExtra[lc])
				if err != nil {
					return fail(err)
				}
				length := lengthBase[lc] + int(extra)
				dc, err := dist.decode(br)
				if err != nil {
					return fail(err)
				}
				if dc >= numDist {
					return fail(ErrCorrupt)
				}
				dx, err := br.readBits(distExtra[dc])
				if err != nil {
					return fail(err)
				}
				d := distBase[dc] + int(dx)
				if d > len(out) {
					return fail(fmt.Errorf("%w: distance %d beyond output start", ErrCorrupt, d))
				}
				for k := 0; k < length; k++ {
					out = append(out, out[len(out)-d])
				}
			}
			if uint64(len(out)) > size {
				return fail(fmt.Errorf("%w: output exceeds declared size", ErrCorrupt))
			}
		}
		if last == 1 {
			break
		}
	}
	br.align()
	if uint64(len(out)) != size {
		return fail(fmt.Errorf("%w: got %d bytes, header says %d", ErrCorrupt, len(out), size))
	}
	if got := crc32.ChecksumIEEE(out); got != want {
		return fail(fmt.Errorf("checksum mismatch: %08x != %08x", got, want))
	}
	if _, err := cr.ReadByte(); err != io.EOF {
		return fail(errors.New("trailing garbage after stream"))
	}
	return out, nil
}
