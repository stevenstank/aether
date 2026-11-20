package ir

import (
	"strings"
	"testing"
)

// TestAllocNoScan checks the noscan flag of Alloc (D-047): printed,
// accepted by the verifier as 0 or 1, and rejected otherwise.
func TestAllocNoScan(t *testing.T) {
	p := &Program{}
	f := p.NewFunc("main", "p.go", Pos{1, 1})
	b := f.Entry()
	f.Alloc(b, f.Const(b, 8, Pos{2, 1}), Pos{2, 1})
	nos := f.AllocNoScan(b, f.Const(b, 8, Pos{3, 1}), Pos{3, 1})
	f.Ret(b, Pos{4, 1})
	if errs := Verify(p); len(errs) > 0 {
		t.Fatal(errs)
	}
	text := p.String()
	if !strings.Contains(text, "v1 = alloc v0 @2:1") || !strings.Contains(text, "v3 = alloc noscan, v2 @3:1") {
		t.Errorf("printed:\n%s", text)
	}
	nos.Const = 2
	if errs := Verify(p); len(errs) != 1 || !strings.Contains(errs[0].Error(), "alloc with flag 2") {
		t.Errorf("flag 2: %v", errs)
	}
}
