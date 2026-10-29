package ir

import (
	"fmt"
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
// A function with parameters or results lists their types:
//
//	func f(i64) i64 @p.go:8:6
//	b0:
//	  v0 = param 0 @8:8
//	  v1 = call f, v0 @9:9
//	  ret v1 @9:2
func (p *Program) String() string {
	var b strings.Builder
	for i, f := range p.Funcs {
		if i > 0 {
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
	fmt.Fprintf(b, " @%s:%s\n", f.File, f.Pos)
	for _, s := range f.Slots {
		fmt.Fprintf(b, "  slot s%d %s %s @%s\n", s.ID, s.Name, s.Type, s.Pos)
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
		default:
			ops = append(ops, fmt.Sprint(in.Const))
		}
	case Load, Store:
		if in.Slot != nil {
			ops = append(ops, fmt.Sprintf("s%d", in.Slot.ID))
		} else {
			ops = append(ops, "s?")
		}
	case Param, Extract:
		ops = append(ops, fmt.Sprint(in.Index))
	case Call:
		if in.Callee != nil {
			ops = append(ops, in.Callee.Name)
		} else {
			ops = append(ops, "?")
		}
	}
	for _, a := range in.Args {
		if a == nil {
			ops = append(ops, "v?")
		} else {
			ops = append(ops, fmt.Sprintf("v%d", a.ID))
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

func types(ts []Type) string {
	s := make([]string, len(ts))
	for i, t := range ts {
		s[i] = t.String()
	}
	return strings.Join(s, ", ")
}
