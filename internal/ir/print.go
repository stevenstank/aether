package ir

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// String returns the textual form of p. The form is deterministic: it
// depends only on the program's contents, so it is suitable for golden
// tests. It is an output format only; there is no parser for it.
//
//	func main @p.go:3:6
//	  slot s0 x i64 @4:2
//	b0:
//	  v0 = const 1 @4:7
//	  store s0, v0 @4:2
//	  v1 = load s0 @5:10
//	  println v1 @5:2
//	  ret @6:1
//
// The program's strings come first, quoted, then the globals, one per line,
// then the functions:
//
//	string s0 "héllo"
//
//	global g0 x i64 @p.go:3:5
//
// Global memory objects (D-034) follow the globals, and a function's own
// follow its slots, with their size and alignment:
//
//	mem gm0 a 24 align 8 @p.go:4:5
//	  mem m0 p 16 align 8 @6:2
//
// A function with parameters or results lists their types:
//
//	func f(i64) i64 @p.go:8:6
//	b0:
//	  v0 = param 0 @8:8
//	  v1 = call f, v0 @9:9
//	  ret v1 @9:2
//
// A function in SSA form (D-025) is marked ssa, and a phi lists its type
// and each incoming edge with its value:
//
//	func f(i64) i64 ssa @p.go:8:6
//	...
//	b3:
//	  v7 = phi i64, b1.0: v2, b2.0: v5 @10:3
func (p *Program) String() string {
	var b strings.Builder
	for i, s := range p.Strings {
		fmt.Fprintf(&b, "string s%d %s\n", i, strconv.Quote(s))
	}
	for _, g := range p.Globals {
		fmt.Fprintf(&b, "global g%d %s %s @%s:%s\n", g.ID, g.Name, g.Type, g.File, g.Pos)
	}
	for _, m := range p.Mems {
		fmt.Fprintf(&b, "mem %s %s %d align %d @%s:%s\n", m, m.Name, m.Size, m.Align, m.File, m.Pos)
	}
	for i, f := range p.Funcs {
		if i > 0 || len(p.Globals)+len(p.Strings)+len(p.Mems) > 0 {
			b.WriteString("\n")
		}
		f.write(&b)
	}
	return b.String()
}

func (f *Func) write(b *strings.Builder) {
	b.WriteString("func " + f.Name)
	if len(f.Params)+len(f.Results) > 0 {
		b.WriteString("(" + types(f.Params) + ")")
		switch len(f.Results) {
		case 0:
		case 1:
			b.WriteString(" " + types(f.Results))
		default:
			b.WriteString(" (" + types(f.Results) + ")")
		}
	}
	if f.SSA {
		b.WriteString(" ssa")
	}
	fmt.Fprintf(b, " @%s:%s\n", f.File, f.Pos)
	for _, s := range f.Slots {
		fmt.Fprintf(b, "  slot s%d %s %s @%s\n", s.ID, s.Name, s.Type, s.Pos)
	}
	for _, m := range f.Mems {
		fmt.Fprintf(b, "  mem %s %s %d align %d @%s\n", m, m.Name, m.Size, m.Align, m.Pos)
	}
	for _, blk := range f.Blocks {
		fmt.Fprintf(b, "b%d:\n", blk.ID)
		for _, in := range blk.Instrs {
			fmt.Fprintf(b, "  %s @%s\n", in, in.Pos)
		}
	}
}

// String returns the instruction without its position.
func (in *Instr) String() string {
	var b strings.Builder
	if in.ID >= 0 {
		fmt.Fprintf(&b, "v%d = ", in.ID)
	}
	b.WriteString(in.Op.String())
	var ops []string
	switch in.Op {
	case Const:
		switch {
		case in.Type == Bool && in.Const == 0:
			ops = append(ops, "false")
		case in.Type == Bool && in.Const == 1:
			ops = append(ops, "true")
		case in.Type == I64:
			ops = append(ops, fmt.Sprint(in.Const))
		case in.Type == F64:
			ops = append(ops, "f64 "+strconv.FormatFloat(math.Float64frombits(uint64(in.Const)), 'g', -1, 64))
		case in.Type == F32:
			ops = append(ops, "f32 "+strconv.FormatFloat(float64(math.Float32frombits(uint32(in.Const))), 'g', -1, 32))
		case in.Type.IsInteger() && !in.Type.Signed():
			ops = append(ops, fmt.Sprintf("%s %d", in.Type, uint64(in.Const)))
		default:
			ops = append(ops, fmt.Sprintf("%s %d", in.Type, in.Const))
		}
	case Conv, IToF, FToF, FToI, LdM:
		ops = append(ops, in.Type.String())
	case Addr:
		if in.Mem != nil {
			ops = append(ops, in.Mem.String())
		} else {
			ops = append(ops, "m?")
		}
	case Panic:
		if in.Index >= 0 && in.Index < len(PanicMessages) {
			ops = append(ops, panicNames[in.Index])
		} else {
			ops = append(ops, fmt.Sprint(in.Index))
		}
	case Load, Store:
		if in.Slot != nil {
			ops = append(ops, fmt.Sprintf("s%d", in.Slot.ID))
		} else {
			ops = append(ops, "s?")
		}
	case GLoad, GStore:
		if in.Global != nil {
			ops = append(ops, fmt.Sprintf("g%d", in.Global.ID))
		} else {
			ops = append(ops, "g?")
		}
	case Param, Extract:
		ops = append(ops, fmt.Sprint(in.Index))
	case Str:
		ops = append(ops, fmt.Sprintf("s%d", in.Index))
	case Call:
		if in.Callee != nil {
			ops = append(ops, in.Callee.Name)
		} else {
			ops = append(ops, "?")
		}
	}
	if in.Op == Phi {
		// Each incoming edge with its value; "?" marks a missing one.
		ops = append(ops, in.Type.String())
		for i := range max(len(in.Args), len(in.Edges)) {
			e, v := "b?.?", "v?"
			if i < len(in.Edges) {
				e = in.Edges[i].String()
			}
			if i < len(in.Args) && in.Args[i] != nil {
				v = fmt.Sprintf("v%d", in.Args[i].ID)
			}
			ops = append(ops, e+": "+v)
		}
	} else {
		for _, a := range in.Args {
			if a == nil {
				ops = append(ops, "v?")
			} else {
				ops = append(ops, fmt.Sprintf("v%d", a.ID))
			}
		}
	}
	for _, t := range in.Targets {
		if t == nil {
			ops = append(ops, "b?")
		} else {
			ops = append(ops, fmt.Sprintf("b%d", t.ID))
		}
	}
	if len(ops) > 0 {
		b.WriteString(" ")
		b.WriteString(strings.Join(ops, ", "))
	}
	return b.String()
}

// String returns m's printed name: mN for a local memory object, gmN for
// a global one.
func (m *Mem) String() string {
	if m.Global {
		return fmt.Sprintf("gm%d", m.ID)
	}
	return fmt.Sprintf("m%d", m.ID)
}

// String returns the edge as bN.I: target I of block N's terminator.
func (e Edge) String() string {
	if e.From == nil {
		return fmt.Sprintf("b?.%d", e.Index)
	}
	return fmt.Sprintf("b%d.%d", e.From.ID, e.Index)
}

func types(ts []Type) string {
	s := make([]string, len(ts))
	for i, t := range ts {
		s[i] = t.String()
	}
	return strings.Join(s, ", ")
}

// panicNames are the printed names of the run-time errors.
var panicNames = [...]string{PanicDivide: "divide", PanicShift: "shift", PanicIndex: "index"}
