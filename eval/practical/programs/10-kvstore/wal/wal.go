// Package wal encodes store operations as a checksummed append-only log.
//
// Each batch is: uvarint payload length, payload, CRC-32 (Castagnoli,
// little endian) of the payload. A payload is a uvarint count followed by
// that many operations: kind byte, uvarint key length, key, and for SET a
// uvarint value length and value.
package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"example.com/kvstore/store"
)

var table = crc32.MakeTable(crc32.Castagnoli)

var ErrCorrupt = errors.New("wal: corrupt record")

type Log struct {
	buf     bytes.Buffer
	Batches int
}

func (l *Log) Append(ops []store.Op) {
	var p []byte
	p = binary.AppendUvarint(p, uint64(len(ops)))
	for _, op := range ops {
		p = append(p, byte(op.Kind))
		p = binary.AppendUvarint(p, uint64(len(op.Key)))
		p = append(p, op.Key...)
		if op.Kind == store.OpSet {
			p = binary.AppendUvarint(p, uint64(len(op.Value)))
			p = append(p, op.Value...)
		}
	}
	var hdr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(hdr[:], uint64(len(p)))
	l.buf.Write(hdr[:n])
	l.buf.Write(p)
	var sum [4]byte
	binary.LittleEndian.PutUint32(sum[:], crc32.Checksum(p, table))
	l.buf.Write(sum[:])
	l.Batches++
}

func (l *Log) Bytes() []byte { return l.buf.Bytes() }
func (l *Log) Size() int     { return l.buf.Len() }

// Corrupt flips one bit at offset, for testing recovery.
func (l *Log) Corrupt(offset int) error {
	b := l.buf.Bytes()
	if offset < 0 || offset >= len(b) {
		return fmt.Errorf("offset %d outside log of %d bytes", offset, len(b))
	}
	b[offset] ^= 0x10
	return nil
}

// Truncate drops the last n bytes, simulating a torn write.
func (l *Log) Truncate(n int) {
	l.buf.Truncate(max(0, l.buf.Len()-n))
}

// Replay decodes batches from data and calls apply for each operation of
// each complete, valid batch. It returns the number of batches applied and
// an error wrapping ErrCorrupt for the first bad batch, or nil at a clean
// end. A torn final batch is reported as io.ErrUnexpectedEOF.
func Replay(data []byte, apply func(store.Op)) (int, error) {
	r := bytes.NewReader(data)
	batches := 0
	for r.Len() > 0 {
		off := len(data) - r.Len()
		n, err := binary.ReadUvarint(r)
		if err != nil {
			return batches, fmt.Errorf("batch %d at offset %d: %w", batches, off, io.ErrUnexpectedEOF)
		}
		if n > uint64(r.Len()) {
			return batches, fmt.Errorf("batch %d at offset %d: length %d: %w", batches, off, n, io.ErrUnexpectedEOF)
		}
		p := make([]byte, n)
		io.ReadFull(r, p)
		var sum [4]byte
		if _, err := io.ReadFull(r, sum[:]); err != nil {
			return batches, fmt.Errorf("batch %d at offset %d: checksum: %w", batches, off, io.ErrUnexpectedEOF)
		}
		if binary.LittleEndian.Uint32(sum[:]) != crc32.Checksum(p, table) {
			return batches, fmt.Errorf("batch %d at offset %d: checksum mismatch: %w", batches, off, ErrCorrupt)
		}
		ops, err := decode(p)
		if err != nil {
			return batches, fmt.Errorf("batch %d at offset %d: %v: %w", batches, off, err, ErrCorrupt)
		}
		for _, op := range ops {
			apply(op)
		}
		batches++
	}
	return batches, nil
}

func decode(p []byte) ([]store.Op, error) {
	r := bytes.NewReader(p)
	readStr := func() (string, error) {
		n, err := binary.ReadUvarint(r)
		if err != nil || n > uint64(r.Len()) {
			return "", errors.New("bad string length")
		}
		b := make([]byte, n)
		io.ReadFull(r, b)
		return string(b), nil
	}
	count, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, errors.New("bad count")
	}
	ops := make([]store.Op, 0, min(count, 1024))
	for i := uint64(0); i < count; i++ {
		kind, err := r.ReadByte()
		if err != nil {
			return nil, errors.New("missing op kind")
		}
		op := store.Op{Kind: store.OpKind(kind)}
		if op.Key, err = readStr(); err != nil {
			return nil, err
		}
		switch op.Kind {
		case store.OpSet:
			if op.Value, err = readStr(); err != nil {
				return nil, err
			}
		case store.OpDel:
		default:
			return nil, fmt.Errorf("unknown op %v", op.Kind)
		}
		ops = append(ops, op)
	}
	if r.Len() != 0 {
		return nil, errors.New("trailing bytes")
	}
	return ops, nil
}
