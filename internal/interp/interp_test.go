package interp

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/ir"
)

// The tests build IR by hand, so the interpreter is tested independently of
// lowering. Lowering is tested against gc through the interpreter in
// cmd/aether.

var p1 = ir.Pos{Line: 1, Col: 1}

// program returns a main whose body is built by body, then ends in ret.
func program(body func(f *ir.Func, b *ir.Block)) *ir.Program {
	p := &ir.Program{}
	f := p.NewFunc("main", "p.go", p1)
	body(f, f.Entry())
	f.Ret(f.Entry(), p1)
	return p
}

func output(t *testing.T, p *ir.Program) string {
	t.Helper()
	var stderr bytes.Buffer
	if err := Run(p, &stderr); err != nil {
		t.Fatal(err)
	}
	return stderr.String()
}

func TestArithmeticWraps(t *testing.T) {
	const max, min = math.MaxInt64, math.MinInt64
	tests := []struct {
		op   ir.Op
		x, y int64
		want string
	}{
		{ir.Add, max, 1, "-9223372036854775808"},
		{ir.Sub, min, 1, "9223372036854775807"},
		{ir.Mul, 3037000500, 3037000500, "-9223372036709301616"},
		{ir.Mul, max, 2, "-2"},
		{ir.Mul, min, -1, "-9223372036854775808"},
		{ir.Add, -5, 3, "-2"},
		{ir.Sub, 3, 5, "-2"},
		{ir.Mul, -4, 6, "-24"},
	}
	for _, tt := range tests {
		got := output(t, program(func(f *ir.Func, b *ir.Block) {
			v := f.Binary(b, tt.op, f.Const(b, tt.x, p1), f.Const(b, tt.y, p1), p1)
			f.Println(b, []*ir.Instr{v}, p1)
		}))
		if got != tt.want+"\n" {
			t.Errorf("%d %s %d: got %q, want %q", tt.x, tt.op, tt.y, got, tt.want)
		}
	}
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, []*ir.Instr{f.Neg(b, f.Const(b, min, p1), p1), f.Neg(b, f.Const(b, max, p1), p1)}, p1)
	}))
	if want := "-9223372036854775808 -9223372036854775807\n"; got != want {
		t.Errorf("neg: got %q, want %q", got, want)
	}
}

func TestPrintlnFormat(t *testing.T) {
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, nil, p1)
		f.Println(b, []*ir.Instr{f.Const(b, 0, p1)}, p1)
		f.Println(b, []*ir.Instr{f.Const(b, -10, p1), f.Const(b, 100, p1), f.Const(b, math.MinInt64, p1)}, p1)
	}))
	if want := "\n0\n-10 100 -9223372036854775808\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSlots(t *testing.T) {
	// x := 1; y := x; x = 2; println(x, y)
	got := output(t, program(func(f *ir.Func, b *ir.Block) {
		x := f.NewSlot("x", ir.I64, p1)
		y := f.NewSlot("y", ir.I64, p1)
		f.Store(b, x, f.Const(b, 1, p1), p1)
		f.Store(b, y, f.Load(b, x, p1), p1)
		f.Store(b, x, f.Const(b, 2, p1), p1)
		f.Println(b, []*ir.Instr{f.Load(b, x, p1), f.Load(b, y, p1)}, p1)
	}))
	if want := "2 1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMalformedNotExecuted(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, []*ir.Instr{f.Const(b, 1, p1)}, p1)
		f.Load(b, f.NewSlot("x", ir.I64, p1), p1) // load before store
	})
	var stderr bytes.Buffer
	err := Run(p, &stderr)
	if err == nil || !strings.Contains(err.Error(), "load of s0 (x) before any store") {
		t.Errorf("got error %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("malformed program produced output %q", stderr.String())
	}
}

type failingWriter struct{ calls int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	return 0, errors.New("write failed")
}

func TestWriteErrorsIgnored(t *testing.T) {
	p := program(func(f *ir.Func, b *ir.Block) {
		f.Println(b, nil, p1)
		f.Println(b, nil, p1)
	})
	w := &failingWriter{}
	if err := Run(p, w); err != nil {
		t.Fatalf("write error reported: %v", err)
	}
	if w.calls != 2 {
		t.Errorf("%d writes, want 2: execution must continue after a failed write", w.calls)
	}
}
