package ir

import (
	"strings"
	"testing"
)

// TestBoundsMessage checks the messages of the index and slice bounds
// errors against gc's, measured on go1.26.1 (D-036).
func TestBoundsMessage(t *testing.T) {
	for _, tt := range []struct {
		kind   int
		x      int64
		signed bool
		y      int64
		want   string
	}{
		{PanicIndex, 4, true, 3, "index out of range [4] with length 3"},
		{PanicIndex, -1, true, 3, "index out of range [-1]"},
		{PanicIndex, -1, false, 3, "index out of range [18446744073709551615] with length 3"},
		{PanicSliceAlen, 6, true, 4, "slice bounds out of range [:6] with length 4"},
		{PanicSliceAcap, 6, true, 5, "slice bounds out of range [:6] with capacity 5"},
		{PanicSliceAcap, -2, true, 5, "slice bounds out of range [:-2]"},
		{PanicSliceB, 4, true, 3, "slice bounds out of range [4:3]"},
		{PanicSliceB, -1, true, 3, "slice bounds out of range [-1:]"},
		{PanicSlice3Alen, 7, true, 4, "slice bounds out of range [::7] with length 4"},
		{PanicSlice3Acap, 7, true, 5, "slice bounds out of range [::7] with capacity 5"},
		{PanicSlice3B, 4, true, 3, "slice bounds out of range [:4:3]"},
		{PanicSlice3B, -4, true, 3, "slice bounds out of range [:-4:]"},
		{PanicSlice3C, 2, true, 1, "slice bounds out of range [2:1:]"},
		{PanicSlice3C, -1, true, 1, "slice bounds out of range [-1::]"},
		{PanicSlice3C, -1, false, 1, "slice bounds out of range [18446744073709551615:1:]"},
	} {
		if got := BoundsMessage(tt.kind, tt.x, tt.signed, tt.y); got != tt.want {
			t.Errorf("BoundsMessage(%s, %d, %v, %d) = %q, want %q", panicNames[tt.kind], tt.x, tt.signed, tt.y, got, tt.want)
		}
	}
	for k := range PanicMessages {
		if IsBounds(k) != (boundsFormats[k] != [4]string{}) {
			t.Errorf("kind %s: IsBounds %v but format %q", panicNames[k], IsBounds(k), boundsFormats[k])
		}
		if panicNames[k] == "" || PanicMessages[k] == "" && k != PanicMsg {
			t.Errorf("kind %d has no name or message", k)
		}
	}
}

// TestVerifySlices checks the verifier's rules for move and for the
// operands of the slice bounds errors.
func TestVerifySlices(t *testing.T) {
	build := func(kind int, moveArg Type, panicArgs int) *Program {
		p := &Program{}
		g := p.NewMem("a", 16, 8, "p.go", Pos{1, 1})
		f := p.NewFunc("main", "p.go", Pos{1, 1})
		b := f.Entry()
		pos := Pos{1, 1}
		a := f.Addr(b, g, pos)
		n := f.TypedConst(b, moveArg, 8, pos)
		f.Move(b, a, f.PAdd(b, a, f.Const(b, 8, pos), pos), n, pos)
		args := []*Instr{f.TypedConst(b, U32, 9, pos), f.Const(b, 3, pos)}
		f.Panic(b, kind, pos, args[:panicArgs]...)
		return p
	}
	valid := build(PanicSlice3C, I64, 2)
	if errs := Verify(valid); len(errs) != 0 {
		t.Fatalf("valid program rejected: %v", errs)
	}
	if got := valid.String(); !strings.Contains(got, "move v0, v3, v1 @1:1") || !strings.Contains(got, "panic slice3c, v4, v5 @1:1") {
		t.Errorf("String():\n%s", got)
	}
	if errs := Verify(build(PanicMakeLen, I64, 0)); len(errs) != 0 {
		t.Errorf("makeslice panic rejected: %v", errs)
	}
	for _, tt := range []struct {
		p    *Program
		want string
	}{
		{build(PanicSliceB, I32, 2), "operand v1 has type i32, want i64"},
		{build(PanicSliceAcap, I64, 0), "has 0 operands, want 2"},
		{build(PanicGrowLen, I64, 2), "has 2 operands, want 0"},
	} {
		var msgs []string
		for _, e := range Verify(tt.p) {
			msgs = append(msgs, e.Error())
		}
		if all := strings.Join(msgs, "\n"); !strings.Contains(all, tt.want) {
			t.Errorf("errors do not mention %q:\n%s", tt.want, all)
		}
	}
}
