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
	fmt.Fprintf(b, "func %s @%s:%s\n", f.Name, f.File, f.Pos)
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
		ops = append(ops, fmt.Sprint(in.Const))
	case Load, Store:
		if in.Slot != nil {
			ops = append(ops, fmt.Sprintf("s%d", in.Slot.ID))
		} else {
			ops = append(ops, "s?")
		}
	}
	for _, a := range in.Args {
		if a == nil {
			ops = append(ops, "v?")
		} else {
			ops = append(ops, fmt.Sprintf("v%d", a.ID))
		}
	}
	if len(ops) > 0 {
		b.WriteString(" ")
		b.WriteString(strings.Join(ops, ", "))
	}
	return b.String()
}
