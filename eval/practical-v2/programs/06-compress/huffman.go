package main

import (
	"container/heap"
	"errors"
	"io"
	"sort"
)

const maxCodeLen = 15

var ErrCorrupt = errors.New("corrupt input")

// bitWriter writes bits least-significant first.
type bitWriter struct {
	w     io.ByteWriter
	acc   uint64
	nbits uint
	err   error
}

func (b *bitWriter) writeBits(v uint32, n uint) {
	b.acc |= uint64(v) << b.nbits
	b.nbits += n
	for b.nbits >= 8 {
		if b.err == nil {
			b.err = b.w.WriteByte(byte(b.acc))
		}
		b.acc >>= 8
		b.nbits -= 8
	}
}

func (b *bitWriter) flush() error {
	if b.nbits > 0 {
		b.writeBits(0, 8-b.nbits%8)
	}
	return b.err
}

type bitReader struct {
	r     io.ByteReader
	acc   uint64
	nbits uint
}

func (b *bitReader) readBits(n uint) (uint32, error) {
	for b.nbits < n {
		c, err := b.r.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		b.acc |= uint64(c) << b.nbits
		b.nbits += 8
	}
	v := uint32(b.acc & (1<<n - 1))
	b.acc >>= n
	b.nbits -= n
	return v, nil
}

// align discards bits up to the next byte boundary.
func (b *bitReader) align() {
	drop := b.nbits % 8
	b.acc >>= drop
	b.nbits -= drop
}

type hnode struct {
	freq  int
	sym   int // -1 for internal nodes
	left  *hnode
	right *hnode
	order int // tie breaker for determinism
}

type nodeHeap []*hnode

func (h nodeHeap) Len() int { return len(h) }
func (h nodeHeap) Less(i, j int) bool {
	if h[i].freq != h[j].freq {
		return h[i].freq < h[j].freq
	}
	return h[i].order < h[j].order
}
func (h nodeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *nodeHeap) Push(x any)   { *h = append(*h, x.(*hnode)) }
func (h *nodeHeap) Pop() any {
	old := *h
	n := old[len(old)-1]
	*h = old[:len(old)-1]
	return n
}

// codeLengths computes Huffman code lengths limited to maxCodeLen bits.
// When the tree is too deep, frequencies are flattened and it retries.
func codeLengths(freq []int) []uint8 {
	lens := make([]uint8, len(freq))
	f := append([]int(nil), freq...)
	for {
		h := &nodeHeap{}
		order := 0
		for s, n := range f {
			if n > 0 {
				*h = append(*h, &hnode{freq: n, sym: s, order: order})
				order++
			}
		}
		switch h.Len() {
		case 0:
			return lens
		case 1:
			lens[(*h)[0].sym] = 1
			return lens
		}
		heap.Init(h)
		for h.Len() > 1 {
			a := heap.Pop(h).(*hnode)
			b := heap.Pop(h).(*hnode)
			heap.Push(h, &hnode{freq: a.freq + b.freq, sym: -1, left: a, right: b, order: order})
			order++
		}
		tooDeep := false
		var walk func(n *hnode, depth uint8)
		walk = func(n *hnode, depth uint8) {
			if n.sym >= 0 {
				if depth > maxCodeLen {
					tooDeep = true
				}
				lens[n.sym] = depth
				return
			}
			walk(n.left, depth+1)
			walk(n.right, depth+1)
		}
		walk((*h)[0], 0)
		if !tooDeep {
			return lens
		}
		for i := range f {
			if f[i] > 0 {
				f[i] = f[i]/2 + 1
			}
		}
	}
}

// canonicalCodes assigns canonical codes (bit-reversed for LSB-first output).
func canonicalCodes(lens []uint8) []uint32 {
	var count [maxCodeLen + 1]int
	for _, l := range lens {
		count[l]++
	}
	count[0] = 0
	var next [maxCodeLen + 1]uint32
	code := uint32(0)
	for bits := 1; bits <= maxCodeLen; bits++ {
		code = (code + uint32(count[bits-1])) << 1
		next[bits] = code
	}
	codes := make([]uint32, len(lens))
	for s, l := range lens {
		if l == 0 {
			continue
		}
		c := next[l]
		next[l]++
		var rev uint32
		for i := uint8(0); i < l; i++ {
			rev = rev<<1 | (c>>i)&1
		}
		codes[s] = rev
	}
	return codes
}

// decoder decodes canonical Huffman codes one bit at a time, in the style
// of zlib's puff.
type decoder struct {
	count  [maxCodeLen + 1]int
	symbol []int
}

func newDecoder(lens []uint8) (*decoder, error) {
	d := &decoder{}
	for _, l := range lens {
		d.count[l]++
	}
	left := 1
	for l := 1; l <= maxCodeLen; l++ {
		left <<= 1
		left -= d.count[l]
		if left < 0 {
			return nil, ErrCorrupt // over-subscribed
		}
	}
	type sl struct {
		sym int
		l   uint8
	}
	var syms []sl
	for s, l := range lens {
		if l > 0 {
			syms = append(syms, sl{s, l})
		}
	}
	sort.SliceStable(syms, func(i, j int) bool { return syms[i].l < syms[j].l })
	for _, x := range syms {
		d.symbol = append(d.symbol, x.sym)
	}
	return d, nil
}

func (d *decoder) decode(br *bitReader) (int, error) {
	code, first, index := 0, 0, 0
	for l := 1; l <= maxCodeLen; l++ {
		bit, err := br.readBits(1)
		if err != nil {
			return 0, err
		}
		code |= int(bit)
		count := d.count[l]
		if code-count < first {
			return d.symbol[index+(code-first)], nil
		}
		index += count
		first += count
		first <<= 1
		code <<= 1
	}
	return 0, ErrCorrupt
}
