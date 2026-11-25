package main

import "testing"

func TestPanicPrefix(t *testing.T) {
	gc := "out\npanic: boom\n\tmore\n\ngoroutine 1 [running]:\nmain.main()\n"
	q, ok := panicPrefix([]byte(gc))
	if !ok || string(q) != "out\npanic: boom\n\tmore\n" {
		t.Fatalf("got %q %v", q, ok)
	}
	q, ok = panicPrefix([]byte("fatal error: all goroutines are asleep - deadlock!\n\ngoroutine 1 [chan receive]:\n"))
	if !ok || string(q) != "fatal error: all goroutines are asleep - deadlock!\n" {
		t.Fatalf("got %q %v", q, ok)
	}
	if _, ok := panicPrefix([]byte("plain error\n")); ok {
		t.Fatal("rule applied without a panic")
	}
}

func TestCompare(t *testing.T) {
	g := runResult{Stdout: []byte("a\n"), Stderr: []byte("panic: x\n\ngoroutine 1:\n"), Exit: 2}
	for _, tc := range []struct {
		a    runResult
		want string
	}{
		{runResult{Stdout: []byte("a\n"), Stderr: []byte("panic: x\n"), Exit: 2}, "PASS"},
		{runResult{Stdout: []byte("a\n"), Stderr: []byte("panic: x\nextra\n"), Exit: 2}, "FAIL"},
		{runResult{Stdout: []byte("b\n"), Stderr: []byte("panic: x\n"), Exit: 2}, "FAIL"},
		{runResult{Stdout: []byte("a\n"), Stderr: []byte("panic: x\n"), Exit: 1}, "FAIL"},
		{runResult{TimedOut: true}, "TIMEOUT"},
	} {
		if got, detail := compare(g, tc.a); got != tc.want {
			t.Errorf("compare(%+v) = %s (%s), want %s", tc.a, got, detail, tc.want)
		}
	}
	plain := runResult{Stdout: []byte("x"), Stderr: []byte("err\n"), Exit: 1}
	if got, _ := compare(plain, runResult{Stdout: []byte("x"), Stderr: []byte("err!\n"), Exit: 1}); got != "FAIL" {
		t.Errorf("stderr difference without a panic: %s", got)
	}
}
