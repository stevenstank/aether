package amd64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/interp"
)

// TestNativeRegisters runs every test program with and without registers,
// natively, and in the interpreter: all three must print the same. With
// registers, f's code must use them and read its operands from them.
func TestNativeRegisters(t *testing.T) {
	for name := range raFuncs {
		p, _ := raBuild(t, name)
		var want bytes.Buffer
		if code, err := interp.Run(p, &want); err != nil || code != 0 {
			t.Fatalf("%s: interpreter: exit %d, %v", name, code, err)
		}
		for _, opts := range []EmitOptions{{}, {Registers: true}} {
			asm, err := EmitWith(p, opts)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			body := string(asm[bytes.Index(asm, []byte("main.f:")):])
			if used := strings.Contains(body, ", %rbx\n"); used != opts.Registers {
				t.Errorf("%s: registers %v, but f writes %%rbx: %v", name, opts.Registers, used)
			}
			if out, code := execute(t, link(t, t.TempDir(), withRuntime(asm)), nil); code != 0 || out != want.String() {
				t.Errorf("%s: registers %v: native exit %d, output differs from the interpreter's:\n%s",
					name, opts.Registers, code, diffLine(out, want.String()))
			}
		}
	}
}
