// Package store is an in-memory ordered key-value store with nested
// transactions.
package store

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

var (
	ErrNotFound   = errors.New("key not found")
	ErrNoTxn      = errors.New("no transaction in progress")
	ErrNotInteger = errors.New("value is not an integer")
	ErrOverflow   = errors.New("increment would overflow")
)

// Op is one change to the store, as recorded in the log.
type Op struct {
	Kind  OpKind
	Key   string
	Value string
}

type OpKind uint8

const (
	OpSet OpKind = iota + 1
	OpDel
)

func (k OpKind) String() string {
	switch k {
	case OpSet:
		return "SET"
	case OpDel:
		return "DEL"
	}
	return fmt.Sprintf("OpKind(%d)", uint8(k))
}

// undo records the previous state of a key.
type undo struct {
	key     string
	value   string
	existed bool
}

// Store keeps keys in sorted order alongside a hash index.
type Store struct {
	data     map[string]string
	keys     []string
	txns     [][]undo
	ops      [][]Op // ops made inside each open transaction
	OnCommit func([]Op)
}

func New() *Store { return &Store{data: map[string]string{}} }

func (s *Store) Len() int { return len(s.data) }

func (s *Store) Get(key string) (string, error) {
	v, ok := s.data[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Store) record(key string) {
	if len(s.txns) == 0 {
		return
	}
	v, ok := s.data[key]
	top := len(s.txns) - 1
	s.txns[top] = append(s.txns[top], undo{key, v, ok})
}

func (s *Store) emit(op Op) {
	if len(s.ops) > 0 {
		top := len(s.ops) - 1
		s.ops[top] = append(s.ops[top], op)
		return
	}
	if s.OnCommit != nil {
		s.OnCommit([]Op{op})
	}
}

func (s *Store) rawSet(key, value string) {
	if _, ok := s.data[key]; !ok {
		i, _ := slices.BinarySearch(s.keys, key)
		s.keys = slices.Insert(s.keys, i, key)
	}
	s.data[key] = value
}

func (s *Store) rawDel(key string) bool {
	if _, ok := s.data[key]; !ok {
		return false
	}
	delete(s.data, key)
	i, _ := slices.BinarySearch(s.keys, key)
	s.keys = slices.Delete(s.keys, i, i+1)
	return true
}

func (s *Store) Set(key, value string) {
	s.record(key)
	s.rawSet(key, value)
	s.emit(Op{OpSet, key, value})
}

func (s *Store) Delete(key string) error {
	if _, ok := s.data[key]; !ok {
		return ErrNotFound
	}
	s.record(key)
	s.rawDel(key)
	s.emit(Op{Kind: OpDel, Key: key})
	return nil
}

// Incr adds delta to the integer value of key (missing keys count as 0).
func (s *Store) Incr(key string, delta int64) (int64, error) {
	var n int64
	if v, ok := s.data[key]; ok {
		var err error
		n, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("INCR %s: %w", key, ErrNotInteger)
		}
	}
	if delta > 0 && n > (1<<63-1)-delta || delta < 0 && n < (-1<<63)-delta {
		return 0, fmt.Errorf("INCR %s by %d: %w", key, delta, ErrOverflow)
	}
	n += delta
	s.Set(key, strconv.FormatInt(n, 10))
	return n, nil
}

// Scan returns up to limit keys with the given prefix, starting at or after
// start, in order.
func (s *Store) Scan(prefix, start string, limit int) []string {
	from := max(prefix, start)
	i, _ := slices.BinarySearch(s.keys, from)
	var out []string
	for ; i < len(s.keys) && len(out) < limit; i++ {
		if !strings.HasPrefix(s.keys[i], prefix) {
			break
		}
		out = append(out, s.keys[i])
	}
	return out
}

func (s *Store) Depth() int { return len(s.txns) }

func (s *Store) Begin() {
	s.txns = append(s.txns, nil)
	s.ops = append(s.ops, nil)
}

func (s *Store) Rollback() error {
	if len(s.txns) == 0 {
		return ErrNoTxn
	}
	top := s.txns[len(s.txns)-1]
	for i := len(top) - 1; i >= 0; i-- {
		u := top[i]
		if u.existed {
			s.rawSet(u.key, u.value)
		} else {
			s.rawDel(u.key)
		}
	}
	s.txns = s.txns[:len(s.txns)-1]
	s.ops = s.ops[:len(s.ops)-1]
	return nil
}

// Commit ends the innermost transaction. Its changes join the enclosing
// transaction, or are emitted if it was the outermost.
func (s *Store) Commit() error {
	if len(s.txns) == 0 {
		return ErrNoTxn
	}
	n := len(s.txns) - 1
	undos, ops := s.txns[n], s.ops[n]
	s.txns, s.ops = s.txns[:n], s.ops[:n]
	if n > 0 {
		s.txns[n-1] = append(s.txns[n-1], undos...)
		s.ops[n-1] = append(s.ops[n-1], ops...)
	} else if len(ops) > 0 && s.OnCommit != nil {
		s.OnCommit(ops)
	}
	return nil
}

// Apply replays logged operations without logging them again.
func (s *Store) Apply(op Op) {
	switch op.Kind {
	case OpSet:
		s.rawSet(op.Key, op.Value)
	case OpDel:
		s.rawDel(op.Key)
	}
}

// Digest is an order-independent fingerprint of the contents (FNV-1a over
// the sorted pairs).
func (s *Store) Digest() uint64 {
	h := uint64(14695981039346656037)
	mix := func(str string) {
		for i := 0; i < len(str); i++ {
			h ^= uint64(str[i])
			h *= 1099511628211
		}
		h ^= 0xff
		h *= 1099511628211
	}
	for _, k := range s.keys {
		mix(k)
		mix(s.data[k])
	}
	return h
}
