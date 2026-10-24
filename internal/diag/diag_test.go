package diag

import (
	"slices"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		d    Diagnostic
		want string
	}{
		{Diagnostic{Class: Invalid, Pos: Pos{"a.go", 3, 7}, Message: "bad"}, "a.go:3:7: invalid: bad"},
		{Diagnostic{Class: Unsupported, Pos: Pos{"a.go", 1, 2}, Message: "x is not supported", Feature: "if"},
			"a.go:1:2: unsupported: x is not supported [if]"},
		{Diagnostic{Class: Unsupported, Pos: Pos{File: "b.go"}, Message: "m", Feature: "multiple-files"},
			"b.go: unsupported: m [multiple-files]"},
		{Diagnostic{Class: Internal, Message: "crash\nsecond line"}, "internal: crash second line"},
		{Diagnostic{Class: Tool, Pos: Pos{"a.go", 4, 0}, Message: "as missing"}, "a.go:4: tool: as missing"},
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestClassString(t *testing.T) {
	for c, want := range map[Class]string{Invalid: "invalid", Unsupported: "unsupported", Internal: "internal", Tool: "tool", 0: "Class(0)"} {
		if got := c.String(); got != want {
			t.Errorf("Class(%d).String() = %q, want %q", int(c), got, want)
		}
	}
}

func TestSort(t *testing.T) {
	ds := []Diagnostic{
		{Class: Unsupported, Pos: Pos{"b.go", 1, 1}, Message: "z"},
		{Class: Unsupported, Pos: Pos{"a.go", 2, 5}, Message: "y", Feature: "b"},
		{Class: Unsupported, Pos: Pos{"a.go", 2, 5}, Message: "y", Feature: "a"},
		{Class: Invalid, Pos: Pos{"a.go", 2, 5}, Message: "y"},
		{Class: Invalid, Pos: Pos{"a.go", 2, 1}, Message: "x"},
		{Class: Invalid, Pos: Pos{"a.go", 10, 1}, Message: "w"},
	}
	Sort(ds)
	var got []string
	for _, d := range ds {
		got = append(got, d.String())
	}
	want := []string{
		"a.go:2:1: invalid: x",
		"a.go:2:5: invalid: y",
		"a.go:2:5: unsupported: y [a]",
		"a.go:2:5: unsupported: y [b]",
		"a.go:10:1: invalid: w",
		"b.go:1:1: unsupported: z",
	}
	if !slices.Equal(got, want) {
		t.Errorf("sorted:\n%q\nwant:\n%q", got, want)
	}
}

func TestExitCode(t *testing.T) {
	d := func(c Class) Diagnostic { return Diagnostic{Class: c} }
	tests := []struct {
		ds   []Diagnostic
		want int
	}{
		{nil, ExitOK},
		{[]Diagnostic{d(Invalid)}, ExitInvalid},
		{[]Diagnostic{d(Unsupported)}, ExitUnsupported},
		{[]Diagnostic{d(Tool)}, ExitTool},
		{[]Diagnostic{d(Internal)}, ExitInternal},
		{[]Diagnostic{d(Unsupported), d(Invalid)}, ExitInvalid},
		{[]Diagnostic{d(Invalid), d(Tool)}, ExitTool},
		{[]Diagnostic{d(Tool), d(Internal), d(Invalid)}, ExitInternal},
	}
	for _, tt := range tests {
		if got := ExitCode(tt.ds); got != tt.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tt.ds, got, tt.want)
		}
	}
	// The codes are a documented contract (D-018).
	if ExitOK != 0 || ExitInvalid != 1 || ExitUsage != 2 || ExitUnsupported != 3 || ExitInternal != 4 || ExitTool != 5 {
		t.Error("exit code values changed")
	}
}
