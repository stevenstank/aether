package main

import "testing"

func TestZZProf(t *testing.T) {
	src := []byte(adversarialAxes[1].gen(128))
	prog, ds := compile([]source{{"l.go", src}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, err := emitProgram(prog); err != nil {
		t.Fatal(err)
	}
}
