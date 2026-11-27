package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// preemptStartProgram ends only if its goroutines, spinning in a loop and
// in recursion with no loop, are preempted while main sleeps.
const preemptStartProgram = `package main

import (
	"fmt"
	"time"
)

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	go func() {
		for {
		}
	}()
	go func() { fmt.Println(fib(90)) }()
	time.Sleep(time.Millisecond)
	fmt.Println("preempted")
}
`

// TestPreemptStart checks how aether.preemptstart sets up preemption
// (D-059, D-060), in a process that TestPreemptStartHelper prepares and
// then replaces with the program: normally; with SIGURG blocked, as a
// parent may leave it across execve, which preemptstart undoes; and with
// RLIMIT_SIGPENDING 0, under which timer_create fails with EAGAIN, so the
// program must end with a fatal error before running any of its code
// rather than run without preemption.
func TestPreemptStart(t *testing.T) {
	requireTools(t)
	dir := t.TempDir()
	for _, o0 := range []bool{false, true} {
		exe := filepath.Join(dir, "start")
		if o0 {
			exe += "0"
		}
		if ds, err := build([]source{{"start.go", []byte(preemptStartProgram)}}, buildOptions{out: exe, o0: o0}, io.Discard); len(ds) > 0 || err != nil {
			t.Fatalf("-O0 %v: not built: %v %v", o0, ds, err)
		}
		for _, tt := range []struct {
			mode string
			want result
		}{
			{"normal", result{stdout: "preempted\n"}},
			{"blocked", result{stdout: "preempted\n"}},
			{"nosigpending", result{stderr: "fatal error: runtime: cannot start preemption: timer_create failed, errno 11\n", exit: 2}},
		} {
			if got := runPreemptHelper(t, exe, tt.mode); got != tt.want {
				t.Errorf("-O0 %v, %s:\n%s", o0, tt.mode, diffResults(got, tt.want))
			}
		}
	}
}

// runPreemptHelper runs exe through TestPreemptStartHelper in mode.
func runPreemptHelper(t *testing.T, exe, mode string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreemptStartHelper$")
	cmd.Env = append(os.Environ(), "AETHER_PREEMPT_HELPER="+mode+":"+exe)
	cmd.WaitDelay = waitDelay
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := result{}
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("%s: killed after 20 s: not preempted", mode)
		}
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() < 0 {
			t.Fatalf("%s: %v", mode, err)
		}
		r.exit = exit.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// TestPreemptStartHelper, run by TestPreemptStart, prepares this process
// as AETHER_PREEMPT_HELPER's mode says and executes its program in it.
func TestPreemptStartHelper(t *testing.T) {
	mode, exe, ok := strings.Cut(os.Getenv("AETHER_PREEMPT_HELPER"), ":")
	if !ok {
		t.Skip("run by TestPreemptStart")
	}
	// The mask is the thread's: the one that calls execve keeps it.
	runtime.LockOSThread()
	switch mode {
	case "normal":
	case "blocked":
		set := uint64(1) << (syscall.SIGURG - 1)
		if _, _, e := syscall.RawSyscall6(syscall.SYS_RT_SIGPROCMASK, 0 /* SIG_BLOCK */, uintptr(unsafe.Pointer(&set)), 0, 8, 0, 0); e != 0 {
			t.Fatal(e)
		}
	case "nosigpending":
		const rlimitSigpending = 11
		if err := syscall.Setrlimit(rlimitSigpending, &syscall.Rlimit{}); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("mode %q", mode)
	}
	t.Fatal(syscall.Exec(exe, []string{exe}, os.Environ()))
}
