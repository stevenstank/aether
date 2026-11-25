package store

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Kind uint8

const (
	Null Kind = iota
	Int
	Float
	Text
)

func (k Kind) String() string {
	return [...]string{"NULL", "INT", "FLOAT", "TEXT"}[k]
}

// Value is a dynamically typed SQL value.
type Value struct {
	K Kind
	I int64
	F float64
	S string
}

func IntV(i int64) Value     { return Value{K: Int, I: i} }
func FloatV(f float64) Value { return Value{K: Float, F: f} }
func TextV(s string) Value   { return Value{K: Text, S: s} }

func (v Value) String() string {
	switch v.K {
	case Int:
		return strconv.FormatInt(v.I, 10)
	case Float:
		if v.F == math.Trunc(v.F) && math.Abs(v.F) < 1e15 {
			return strconv.FormatFloat(v.F, 'f', 1, 64)
		}
		return strconv.FormatFloat(v.F, 'g', 10, 64)
	case Text:
		return v.S
	}
	return "NULL"
}

func (v Value) Num() (float64, bool) {
	switch v.K {
	case Int:
		return float64(v.I), true
	case Float:
		return v.F, true
	}
	return 0, false
}

// Compare orders NULL < numbers < text; ints and floats compare numerically.
func Compare(a, b Value) int {
	if a.K == Int && b.K == Int {
		return cmp.Compare(a.I, b.I)
	}
	an, aok := a.Num()
	bn, bok := b.Num()
	switch {
	case aok && bok:
		return cmp.Compare(an, bn)
	case a.K != b.K:
		rank := func(v Value) int {
			switch v.K {
			case Null:
				return 0
			case Text:
				return 2
			}
			return 1
		}
		return cmp.Compare(rank(a), rank(b))
	case a.K == Text:
		return strings.Compare(a.S, b.S)
	}
	return 0
}

type Column struct {
	Name    string
	Type    Kind
	Primary bool
	Unique  bool
	NotNull bool
}

type Row []Value

// indexKey makes non-unique index keys distinct by appending the row id.
type indexKey struct {
	v  Value
	id int64
}

type Index struct {
	Name   string
	Col    int
	Unique bool
	tree   *BTree[indexKey, struct{}]
}

type Table struct {
	Name    string
	Cols    []Column
	rows    *BTree[int64, Row]
	Indexes []*Index
	nextID  int64
	log     *UndoLog
}

var (
	ErrConstraint = errors.New("constraint violation")
	ErrType       = errors.New("type mismatch")
)

func newIndexTree() *BTree[indexKey, struct{}] {
	return NewBTree[indexKey, struct{}](8, func(a, b indexKey) int {
		if c := Compare(a.v, b.v); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})
}

func NewTable(name string, cols []Column, log *UndoLog) *Table {
	t := &Table{Name: name, Cols: cols, rows: NewBTree[int64, Row](16, cmp.Compare[int64]), nextID: 1, log: log}
	for i, c := range cols {
		if c.Primary || c.Unique {
			kind := "unique"
			if c.Primary {
				kind = "pk"
			}
			t.Indexes = append(t.Indexes, &Index{Name: fmt.Sprintf("%s_%s_%s", name, c.Name, kind), Col: i, Unique: true, tree: newIndexTree()})
		}
	}
	return t
}

func (t *Table) ColIndex(name string) int {
	for i, c := range t.Cols {
		if strings.EqualFold(c.Name, name) {
			return i
		}
	}
	return -1
}

func (t *Table) Len() int { return t.rows.Len() }

func (t *Table) CreateIndex(name string, col int, unique bool) error {
	for _, ix := range t.Indexes {
		if strings.EqualFold(ix.Name, name) {
			return fmt.Errorf("index %s already exists", name)
		}
	}
	ix := &Index{Name: name, Col: col, Unique: unique, tree: newIndexTree()}
	for id, row := range t.rows.All() {
		if unique && row[col].K != Null && ix.has(row[col]) {
			return fmt.Errorf("%w: cannot create unique index %s: duplicate %s", ErrConstraint, name, row[col])
		}
		ix.tree.Put(indexKey{row[col], id}, struct{}{})
	}
	t.Indexes = append(t.Indexes, ix)
	return nil
}

func (ix *Index) has(v Value) bool {
	found := false
	from := indexKey{v, math.MinInt64}
	ix.tree.Ascend(&from, func(k indexKey, _ struct{}) bool {
		found = Compare(k.v, v) == 0
		return false
	})
	return found
}

// Scan visits row ids whose indexed value lies in [lo, hi]; nil bounds are
// open. Exclusive bounds are applied by the caller's filter.
func (ix *Index) Scan(lo, hi *Value, fn func(id int64) bool) {
	var from *indexKey
	if lo != nil {
		from = &indexKey{*lo, math.MinInt64}
	}
	ix.tree.Ascend(from, func(k indexKey, _ struct{}) bool {
		if k.v.K == Null && lo == nil && hi != nil {
			return true // NULLs sort first but never satisfy a range
		}
		if hi != nil && Compare(k.v, *hi) > 0 {
			return false
		}
		return fn(k.id)
	})
}

func (ix *Index) Len() int        { return ix.tree.Len() }
func (ix *Index) Height() int     { return ix.tree.Height() }
func (t *Table) RowCheck() string { return t.rows.Check() }

func (t *Table) Get(id int64) (Row, bool) { return t.rows.Get(id) }

// Scan visits all rows in row-id order.
func (t *Table) Scan(fn func(id int64, r Row) bool) { t.rows.Ascend(nil, fn) }

func (t *Table) coerce(row Row) error {
	for i, c := range t.Cols {
		v := row[i]
		switch {
		case v.K == Null:
			if c.NotNull || c.Primary {
				return fmt.Errorf("%w: %s.%s may not be NULL", ErrConstraint, t.Name, c.Name)
			}
		case c.Type == Float && v.K == Int:
			row[i] = FloatV(float64(v.I))
		case c.Type == Int && v.K == Float && v.F == math.Trunc(v.F):
			row[i] = IntV(int64(v.F))
		case v.K != c.Type:
			return fmt.Errorf("%w: %s.%s is %s, got %s %q", ErrType, t.Name, c.Name, c.Type, v.K, v.String())
		}
	}
	return nil
}

func (t *Table) checkUnique(row Row, self int64) error {
	for _, ix := range t.Indexes {
		v := row[ix.Col]
		if !ix.Unique || v.K == Null {
			continue
		}
		var clash bool
		ix.Scan(&v, &v, func(id int64) bool {
			if id != self {
				clash = true
				return false
			}
			return true
		})
		if clash {
			return fmt.Errorf("%w: duplicate value %s for %s.%s", ErrConstraint, v, t.Name, t.Cols[ix.Col].Name)
		}
	}
	return nil
}

func (t *Table) Insert(row Row) (int64, error) {
	if len(row) != len(t.Cols) {
		return 0, fmt.Errorf("table %s has %d columns, got %d values", t.Name, len(t.Cols), len(row))
	}
	row = append(Row(nil), row...)
	if err := t.coerce(row); err != nil {
		return 0, err
	}
	if err := t.checkUnique(row, -1); err != nil {
		return 0, err
	}
	id := t.nextID
	t.nextID++
	t.put(id, row)
	t.log.record(undo{table: t, id: id})
	return id, nil
}

func (t *Table) put(id int64, row Row) {
	t.rows.Put(id, row)
	for _, ix := range t.Indexes {
		ix.tree.Put(indexKey{row[ix.Col], id}, struct{}{})
	}
}

func (t *Table) remove(id int64) Row {
	row, ok := t.rows.Get(id)
	if !ok {
		return nil
	}
	t.rows.Delete(id)
	for _, ix := range t.Indexes {
		ix.tree.Delete(indexKey{row[ix.Col], id})
	}
	return row
}

func (t *Table) Delete(id int64) bool {
	row := t.remove(id)
	if row == nil {
		return false
	}
	t.log.record(undo{table: t, id: id, old: row, deleted: true})
	return true
}

func (t *Table) Update(id int64, row Row) error {
	row = append(Row(nil), row...)
	if err := t.coerce(row); err != nil {
		return err
	}
	if err := t.checkUnique(row, id); err != nil {
		return err
	}
	old := t.remove(id)
	t.put(id, row)
	t.log.record(undo{table: t, id: id, old: old})
	return nil
}

// Snapshot is the exported, gob-friendly form of a table.
type Snapshot struct {
	Name    string
	Cols    []Column
	IDs     []int64
	Rows    []Row
	Indexes []IndexDef
	NextID  int64
}

type IndexDef struct {
	Name   string
	Col    int
	Unique bool
}

func (t *Table) Snapshot() Snapshot {
	s := Snapshot{Name: t.Name, Cols: t.Cols, NextID: t.nextID}
	for id, r := range t.rows.All() {
		s.IDs = append(s.IDs, id)
		s.Rows = append(s.Rows, r)
	}
	for _, ix := range t.Indexes {
		if !strings.HasSuffix(ix.Name, "_pk") && !strings.HasSuffix(ix.Name, "_unique") {
			s.Indexes = append(s.Indexes, IndexDef{ix.Name, ix.Col, ix.Unique})
		}
	}
	return s
}

func FromSnapshot(s Snapshot, log *UndoLog) (*Table, error) {
	t := NewTable(s.Name, s.Cols, log)
	for _, d := range s.Indexes {
		if err := t.CreateIndex(d.Name, d.Col, d.Unique); err != nil {
			return nil, err
		}
	}
	for i, id := range s.IDs {
		t.put(id, s.Rows[i])
	}
	t.nextID = s.NextID
	return t, nil
}

// UndoLog records changes so transactions can roll back.
type undo struct {
	table   *Table
	id      int64
	old     Row // nil for an insert
	deleted bool
}

type UndoLog struct {
	entries []undo
	marks   []int
}

func (l *UndoLog) record(u undo) {
	if len(l.marks) > 0 {
		l.entries = append(l.entries, u)
	}
}

func (l *UndoLog) Depth() int { return len(l.marks) }

func (l *UndoLog) Begin() { l.marks = append(l.marks, len(l.entries)) }

// Commit drops the newest savepoint; changes stay in the log for any
// enclosing transaction.
func (l *UndoLog) Commit() error {
	if len(l.marks) == 0 {
		return errors.New("no transaction is active")
	}
	l.marks = l.marks[:len(l.marks)-1]
	if len(l.marks) == 0 {
		l.entries = l.entries[:0]
	}
	return nil
}

// Rollback undoes changes back to the newest savepoint and returns how
// many were undone.
func (l *UndoLog) Rollback() (int, error) {
	if len(l.marks) == 0 {
		return 0, errors.New("no transaction is active")
	}
	mark := l.marks[len(l.marks)-1]
	l.marks = l.marks[:len(l.marks)-1]
	n := 0
	for i := len(l.entries) - 1; i >= mark; i-- {
		u := l.entries[i]
		u.table.remove(u.id)
		if u.old != nil {
			u.table.put(u.id, u.old)
		}
		n++
	}
	l.entries = l.entries[:mark]
	return n, nil
}
