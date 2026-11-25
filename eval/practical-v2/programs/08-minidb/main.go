// Command minidb runs SQL scripts against an in-memory database with
// B-tree indexes, nested transactions, and gob checkpoints.
//
// Usage: minidb [-echo] [-q] [script.sql]   (reads stdin when no file is given)
package main

import (
	"bufio"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"example.com/minidb/query"
	"example.com/minidb/store"
)

type result struct {
	cols []string
	rows [][]string
	msg  string
}

func kindOf(t string) store.Kind {
	switch t {
	case "INT":
		return store.Int
	case "FLOAT":
		return store.Float
	}
	return store.Text
}

func (db *DB) exec(s query.Stmt) (*result, error) {
	switch s := s.(type) {
	case *query.CreateTable:
		if db.log.Depth() > 0 {
			return nil, errors.New("CREATE TABLE is not allowed inside a transaction")
		}
		key := strings.ToLower(s.Name)
		if _, ok := db.tables[key]; ok {
			return nil, fmt.Errorf("table %s already exists", s.Name)
		}
		var cols []store.Column
		seen := map[string]bool{}
		for _, c := range s.Cols {
			if seen[strings.ToLower(c.Name)] {
				return nil, fmt.Errorf("duplicate column %s", c.Name)
			}
			seen[strings.ToLower(c.Name)] = true
			cols = append(cols, store.Column{Name: c.Name, Type: kindOf(c.Type), Primary: c.Primary, Unique: c.Unique, NotNull: c.NotNull})
		}
		db.tables[key] = store.NewTable(s.Name, cols, db.log)
		return &result{msg: "created table " + s.Name}, nil
	case *query.DropTable:
		if db.log.Depth() > 0 {
			return nil, errors.New("DROP TABLE is not allowed inside a transaction")
		}
		if _, err := db.table(s.Name); err != nil {
			return nil, err
		}
		delete(db.tables, strings.ToLower(s.Name))
		return &result{msg: "dropped table " + s.Name}, nil
	case *query.CreateIndex:
		if db.log.Depth() > 0 {
			return nil, errors.New("CREATE INDEX is not allowed inside a transaction")
		}
		t, err := db.table(s.Table)
		if err != nil {
			return nil, err
		}
		ci := t.ColIndex(s.Col)
		if ci < 0 {
			return nil, fmt.Errorf("no column %s in %s", s.Col, s.Table)
		}
		if err := t.CreateIndex(s.Name, ci, s.Unique); err != nil {
			return nil, err
		}
		return &result{msg: fmt.Sprintf("created index %s on %s(%s)", s.Name, t.Name, t.Cols[ci].Name)}, nil
	case *query.Insert:
		return db.atomically(func() (*result, error) { return db.insert(s) })
	case *query.Update:
		return db.atomically(func() (*result, error) { return db.update(s) })
	case *query.Delete:
		return db.atomically(func() (*result, error) { return db.delete(s) })
	case *query.Select:
		return db.selectRows(s)
	case *query.Begin:
		db.log.Begin()
		return &result{msg: fmt.Sprintf("begin (depth %d)", db.log.Depth())}, nil
	case *query.Commit:
		if err := db.log.Commit(); err != nil {
			return nil, err
		}
		return &result{msg: fmt.Sprintf("commit (depth %d)", db.log.Depth())}, nil
	case *query.Rollback:
		n, err := db.log.Rollback()
		if err != nil {
			return nil, err
		}
		return &result{msg: fmt.Sprintf("rollback: undid %d changes (depth %d)", n, db.log.Depth())}, nil
	case *query.Checkpoint:
		return db.checkpoint()
	case *query.ShowTables:
		r := &result{cols: []string{"table", "rows", "indexes", "height", "check"}}
		for _, name := range slices.Sorted(maps.Keys(db.tables)) {
			t := db.tables[name]
			var ixs []string
			check := "ok"
			if p := t.RowCheck(); p != "" {
				check = p
			}
			h := 0
			for _, ix := range t.Indexes {
				ixs = append(ixs, fmt.Sprintf("%s(%d)", ix.Name, ix.Len()))
				h = max(h, ix.Height())
				if ix.Len() != t.Len() {
					check = "index size mismatch"
				}
			}
			r.rows = append(r.rows, []string{t.Name, fmt.Sprint(t.Len()), strings.Join(ixs, " "), fmt.Sprint(h), check})
		}
		return r, nil
	}
	return nil, fmt.Errorf("unsupported statement %T", s)
}

// atomically runs one statement under a savepoint so a failure leaves no
// partial changes.
func (db *DB) atomically(f func() (*result, error)) (*result, error) {
	db.log.Begin()
	r, err := f()
	if err != nil {
		db.log.Rollback()
		return nil, err
	}
	db.log.Commit()
	return r, nil
}

func (db *DB) insert(s *query.Insert) (*result, error) {
	t, err := db.table(s.Table)
	if err != nil {
		return nil, err
	}
	pos := make([]int, len(t.Cols))
	if s.Cols == nil {
		for i := range pos {
			pos[i] = i
		}
	} else {
		for i := range pos {
			pos[i] = -1
		}
		for k, c := range s.Cols {
			ci := t.ColIndex(c)
			if ci < 0 {
				return nil, fmt.Errorf("no column %s in %s", c, t.Name)
			}
			pos[ci] = k
		}
	}
	for _, exprs := range s.Rows {
		want := len(t.Cols)
		if s.Cols != nil {
			want = len(s.Cols)
		}
		if len(exprs) != want {
			return nil, fmt.Errorf("expected %d values, got %d", want, len(exprs))
		}
		row := make(store.Row, len(t.Cols))
		for i, p := range pos {
			if p < 0 {
				continue
			}
			v, err := (&env{t: t}).eval(exprs[p])
			if err != nil {
				return nil, err
			}
			row[i] = v
		}
		if _, err := t.Insert(row); err != nil {
			return nil, err
		}
	}
	return &result{msg: fmt.Sprintf("inserted %d rows", len(s.Rows))}, nil
}

func (db *DB) update(s *query.Update) (*result, error) {
	t, err := db.table(s.Table)
	if err != nil {
		return nil, err
	}
	cols := make([]int, len(s.Set))
	for i, c := range s.Set {
		if cols[i] = t.ColIndex(c); cols[i] < 0 {
			return nil, fmt.Errorf("no column %s in %s", c, t.Name)
		}
	}
	ids, _, err := db.matching(t, s.Where)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		old, _ := t.Get(id)
		row := slices.Clone(old)
		for i, e := range s.Exprs {
			v, err := (&env{t: t, row: old}).eval(e)
			if err != nil {
				return nil, err
			}
			row[cols[i]] = v
		}
		if err := t.Update(id, row); err != nil {
			return nil, err
		}
	}
	return &result{msg: fmt.Sprintf("updated %d rows", len(ids))}, nil
}

func (db *DB) delete(s *query.Delete) (*result, error) {
	t, err := db.table(s.Table)
	if err != nil {
		return nil, err
	}
	ids, _, err := db.matching(t, s.Where)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		t.Delete(id)
	}
	return &result{msg: fmt.Sprintf("deleted %d rows", len(ids))}, nil
}

func (db *DB) selectRows(s *query.Select) (*result, error) {
	t, err := db.table(s.Table)
	if err != nil {
		return nil, err
	}
	ids, p, err := db.matching(t, s.Where)
	if err != nil {
		return nil, err
	}
	if s.Explain {
		return &result{msg: "plan: " + p.desc}, nil
	}
	items := s.Items
	if s.Star {
		for _, c := range t.Cols {
			items = append(items, query.SelectItem{X: &query.ColRef{Name: c.Name}})
		}
	}
	r := &result{}
	for _, it := range items {
		name := it.Alias
		if name == "" {
			name = it.X.String()
		}
		r.cols = append(r.cols, name)
	}
	rows := make([]store.Row, len(ids))
	for i, id := range ids {
		rows[i], _ = t.Get(id)
	}

	grouped := len(s.GroupBy) > 0 || slices.ContainsFunc(items, func(it query.SelectItem) bool { return hasAggregate(it.X) })
	type outRow struct {
		vals []store.Value
		keys []store.Value
	}
	var out []outRow
	evalOrder := func(en *env, vals []store.Value) ([]store.Value, error) {
		var keys []store.Value
		for _, o := range s.OrderBy {
			// ORDER BY may name an output column or alias.
			if cr, ok := o.X.(*query.ColRef); ok {
				if k := slices.Index(r.cols, cr.Name); k >= 0 {
					keys = append(keys, vals[k])
					continue
				}
			}
			if l, ok := o.X.(*query.Lit); ok && l.Kind == "int" {
				var k int
				fmt.Sscan(l.Text, &k)
				if k < 1 || k > len(vals) {
					return nil, fmt.Errorf("ORDER BY position %d is out of range", k)
				}
				keys = append(keys, vals[k-1])
				continue
			}
			v, err := en.eval(o.X)
			if err != nil {
				return nil, err
			}
			keys = append(keys, v)
		}
		return keys, nil
	}
	if grouped {
		groups := map[string][]store.Row{}
		var order []string
		for _, row := range rows {
			en := &env{t: t, row: row}
			var parts []string
			for _, g := range s.GroupBy {
				v, err := en.eval(g)
				if err != nil {
					return nil, err
				}
				parts = append(parts, fmt.Sprintf("%d:%s", v.K, v))
			}
			k := strings.Join(parts, "\x00")
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], row)
		}
		if len(s.GroupBy) == 0 && len(order) == 0 {
			order = append(order, "")
			groups[""] = []store.Row{}
		}
		for _, k := range order {
			g := groups[k]
			var first store.Row
			if len(g) > 0 {
				first = g[0]
			}
			en := &env{t: t, row: first, group: g}
			if s.Having != nil {
				hv, err := en.eval(s.Having)
				if err != nil {
					return nil, err
				}
				if !truth(hv) {
					continue
				}
			}
			var vals []store.Value
			for _, it := range items {
				v, err := en.eval(it.X)
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
			}
			keys, err := evalOrder(en, vals)
			if err != nil {
				return nil, err
			}
			out = append(out, outRow{vals, keys})
		}
	} else {
		for _, row := range rows {
			en := &env{t: t, row: row}
			var vals []store.Value
			for _, it := range items {
				v, err := en.eval(it.X)
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
			}
			keys, err := evalOrder(en, vals)
			if err != nil {
				return nil, err
			}
			out = append(out, outRow{vals, keys})
		}
	}
	if len(s.OrderBy) > 0 {
		sort.SliceStable(out, func(i, j int) bool {
			for k, o := range s.OrderBy {
				c := store.Compare(out[i].keys[k], out[j].keys[k])
				if c != 0 {
					return (c < 0) != o.Desc
				}
			}
			return false
		})
	}
	if s.Offset > 0 {
		out = out[min(s.Offset, len(out)):]
	}
	if s.Limit >= 0 && len(out) > s.Limit {
		out = out[:s.Limit]
	}
	for _, o := range out {
		cells := make([]string, len(o.vals))
		for i, v := range o.vals {
			cells[i] = v.String()
		}
		r.rows = append(r.rows, cells)
	}
	return r, nil
}

// checkpoint serializes every table with encoding/gob to a temporary file,
// loads it back into a fresh database, and continues with the copy.
func (db *DB) checkpoint() (*result, error) {
	if db.log.Depth() > 0 {
		return nil, errors.New("CHECKPOINT is not allowed inside a transaction")
	}
	f, err := os.CreateTemp("", "minidb-*.gob")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	var snaps []store.Snapshot
	for _, name := range slices.Sorted(maps.Keys(db.tables)) {
		snaps = append(snaps, db.tables[name].Snapshot())
	}
	if err := gob.NewEncoder(f).Encode(snaps); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var loaded []store.Snapshot
	err = gob.NewDecoder(bufio.NewReader(f)).Decode(&loaded)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("checkpoint reload: %w", err)
	}
	fresh := NewDB()
	rows := 0
	for _, s := range loaded {
		t, err := store.FromSnapshot(s, fresh.log)
		if err != nil {
			return nil, err
		}
		fresh.tables[strings.ToLower(s.Name)] = t
		rows += t.Len()
	}
	*db = *fresh
	return &result{msg: fmt.Sprintf("checkpoint: %d tables, %d rows reloaded", len(loaded), rows)}, nil
}

func printResult(w *bufio.Writer, r *result) {
	if r.msg != "" {
		fmt.Fprintln(w, r.msg)
		return
	}
	width := make([]int, len(r.cols))
	for i, c := range r.cols {
		width[i] = utf8.RuneCountInString(c)
	}
	for _, row := range r.rows {
		for i, c := range row {
			width[i] = max(width[i], utf8.RuneCountInString(c))
		}
	}
	line := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				w.WriteString(" | ")
			}
			w.WriteString(c + strings.Repeat(" ", width[i]-utf8.RuneCountInString(c)))
		}
		w.WriteString("\n")
	}
	line(r.cols)
	seps := make([]string, len(width))
	for i, n := range width {
		seps[i] = strings.Repeat("-", n)
	}
	w.WriteString(strings.Join(seps, "-+-") + "\n")
	for _, row := range r.rows {
		line(row)
	}
	fmt.Fprintf(w, "(%d rows)\n", len(r.rows))
}

func main() {
	echo := flag.Bool("echo", false, "print each statement before its result")
	quiet := flag.Bool("q", false, "print only query results and errors")
	flag.Parse()
	var in io.Reader = os.Stdin
	if flag.NArg() > 0 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "minidb:", err)
			os.Exit(1)
		}
		defer f.Close()
		in = f
	}
	src, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "minidb:", err)
		os.Exit(1)
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	stmts, texts, perr := query.ParseScript(string(src))
	db := NewDB()
	failures := 0
	for i, s := range stmts {
		if *echo {
			fmt.Fprintf(w, "> %s\n", texts[i])
		}
		r, err := db.exec(s)
		if err != nil {
			failures++
			var ee *ExecError
			if !errors.As(err, &ee) {
				err = &ExecError{Stmt: texts[i], Err: err}
			}
			kind := "error"
			switch {
			case errors.Is(err, store.ErrConstraint):
				kind = "constraint"
			case errors.Is(err, store.ErrType):
				kind = "type"
			case errors.Is(err, ErrNoTable):
				kind = "missing"
			}
			fmt.Fprintf(w, "%s: %v\n", kind, err)
			continue
		}
		if *quiet && r.msg != "" {
			continue
		}
		printResult(w, r)
	}
	if db.log.Depth() > 0 {
		fmt.Fprintf(w, "warning: %d open transaction(s) discarded at end of script\n", db.log.Depth())
	}
	if perr != nil {
		w.Flush()
		var se *query.SyntaxError
		if errors.As(perr, &se) {
			line := strings.Count(string(src[:min(se.Pos, len(src))]), "\n") + 1
			fmt.Fprintf(os.Stderr, "minidb: line %d: %v\n", line, perr)
		} else {
			fmt.Fprintln(os.Stderr, "minidb:", perr)
		}
		os.Exit(2)
	}
	if failures > 0 {
		w.Flush()
		fmt.Fprintf(os.Stderr, "minidb: %d statements failed\n", failures)
		os.Exit(1)
	}
}
