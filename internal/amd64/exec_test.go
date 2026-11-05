package amd64

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runTimeout bounds every run of a test executable, directly or through
// /bin/sh. The programs these tests run finish in well under a second, so
// a miscompiled loop fails its test after a minute instead of hanging the
// package until go test's own timeout.
const runTimeout = time.Minute

// waitDelay bounds the wait for a killed process's output pipes, which a
// child of /bin/sh could still hold.
const waitDelay = 5 * time.Second

// timedCmd is a command that is killed when its timeout passes or when the
// test that made it ends, whichever comes first, so a test that fails or
// stops before waiting for it leaves no process running.
type timedCmd struct {
	*exec.Cmd
	ctx     context.Context
	timeout time.Duration
}

// command returns a timedCmd running name with args, with runTimeout.
func command(t *testing.T, name string, args ...string) timedCmd {
	return commandTimeout(t, runTimeout, name, args...)
}

func commandTimeout(t *testing.T, timeout time.Duration, name string, args ...string) timedCmd {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = waitDelay
	return timedCmd{cmd, ctx, timeout}
}

// timedOut reports whether err, the result of running or waiting for c,
// comes from killing it at its timeout.
func (c timedCmd) timedOut(err error) bool {
	return err != nil && errors.Is(c.ctx.Err(), context.DeadlineExceeded)
}

// checkTimeout fails the test if err comes from killing c at its timeout,
// naming the command, which includes the executable's path.
func (c timedCmd) checkTimeout(t *testing.T, err error) {
	t.Helper()
	if c.timedOut(err) {
		t.Fatal(c.timeoutMessage())
	}
}

func (c timedCmd) timeoutMessage() string {
	return fmt.Sprintf("%s: killed after running for the %v timeout", strings.Join(c.Args, " "), c.timeout)
}

// loopForever links an executable whose main.main never returns.
func loopForever(t *testing.T) string {
	return link(t, t.TempDir(), withRuntime([]byte("\t.text\n\t.globl\tmain.main\nmain.main:\n\tjmp\tmain.main\n"+mainStack(16))))
}

// TestCommandTimeout checks that a command that runs past its timeout is
// killed and reported as timed out, and that one that finishes is not.
func TestCommandTimeout(t *testing.T) {
	exe := loopForever(t)
	c := commandTimeout(t, 200*time.Millisecond, exe)
	start := time.Now()
	err := c.Run()
	if elapsed := time.Since(start); elapsed > runTimeout/2 {
		t.Errorf("Run returned after %v", elapsed)
	}
	if !c.timedOut(err) {
		t.Fatalf("Run: %v; want a timeout", err)
	}
	if ws := c.ProcessState.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Errorf("process state %v, want killed by SIGKILL", c.ProcessState)
	}
	if msg := c.timeoutMessage(); !strings.Contains(msg, exe) || !strings.Contains(msg, "200ms") {
		t.Errorf("message %q does not name the executable and the timeout", msg)
	}

	done := command(t, "/bin/sh", "-c", "exit 3")
	if err := done.Run(); done.timedOut(err) {
		t.Errorf("a command that exited with status 3 was reported as timed out: %v", err)
	}
}

// TestCommandKilledAtTestEnd checks that a process a test starts and never
// waits for is killed when that test ends early, as a failing test does.
func TestCommandKilledAtTestEnd(t *testing.T) {
	exe := loopForever(t)
	var c timedCmd
	if !t.Run("stops early", func(t *testing.T) {
		c = command(t, exe)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		// SkipNow ends the subtest through runtime.Goexit, as FailNow
		// does, without failing this test.
		t.SkipNow()
	}) {
		return
	}
	errc := make(chan error, 1)
	go func() { errc <- c.Wait() }()
	select {
	case err := <-errc:
		if ws := c.ProcessState.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
			t.Errorf("Wait: %v; want killed by SIGKILL", err)
		}
	case <-time.After(runTimeout / 2):
		c.Process.Kill()
		t.Fatal("the process outlived the test that started it")
	}
}
