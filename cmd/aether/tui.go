package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/tui"
)

// runTUI is "aether tui [DIR]" (D-061). The TUI runs the same functions as
// aether check and aether build, in this process, through runRequest.
func runTUI(args []string, stderr io.Writer) int {
	if len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-")) {
		fmt.Fprintf(stderr, "aether tui: usage: aether tui [DIR]\n")
		return diag.ExitUsage
	}
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	root, err := filepath.Abs(dir)
	if err == nil {
		var fi os.FileInfo
		if fi, err = os.Stat(root); err == nil && !fi.IsDir() {
			err = fmt.Errorf("%s is not a directory", dir)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "aether tui: %v\n", err)
		return diag.ExitUsage
	}
	err = tui.Run(tui.Config{Root: root, Run: runRequest, CommandLine: commandLine})
	if err != nil {
		fmt.Fprintf(stderr, "aether tui: %v\n", err)
		if errors.Is(err, tui.ErrNoTerminal) {
			fmt.Fprintf(stderr, "aether tui: use aether check or aether build instead\n")
		}
		return diag.ExitUsage
	}
	return diag.ExitOK
}

// commandLine returns the arguments of the aether command that does what
// r does.
func commandLine(r tui.Request) []string {
	args := []string{r.Mode.String()}
	if r.Mode == tui.Build {
		if r.Options.Commands {
			args = append(args, "-x")
		}
		if r.Options.KeepWork {
			args = append(args, "-work")
		}
		if r.Options.O0 {
			args = append(args, "-O0")
		}
		args = append(args, "-o", r.Output)
	}
	return append(args, r.Files...)
}

// buildStages are the TUI's names of build's steps.
var buildStages = [...]tui.Stage{
	stepCheck:    tui.StageCheck,
	stepEmit:     tui.StageEmit,
	stepAssemble: tui.StageAssemble,
	stepLink:     tui.StageLink,
	stepPublish:  tui.StageWrite,
}

// runRequest runs r as runCheck or runBuild runs commandLine(r), and
// returns what they would report: the sorted diagnostics, or the usage
// error, and the exit status. What they write to stderr besides the
// diagnostics, such as -x's log, goes to p line by line.
func runRequest(r tui.Request, p tui.Progress) tui.Result {
	cmd := r.Mode.String()
	usage := func(msg string) tui.Result {
		msg = strings.TrimPrefix(strings.TrimSpace(msg), "aether "+cmd+": ")
		return tui.Result{Err: errors.New(msg), Exit: diag.ExitUsage}
	}
	p.Stage(tui.StageRead, 0, 0)
	var msg bytes.Buffer
	srcs, err := readSources(cmd, r.Files, &msg)
	if err != nil {
		return usage(msg.String())
	}
	var ds []diag.Diagnostic
	if r.Mode == tui.Check {
		p.Stage(tui.StageCheck, 0, 0)
		if ds, err = checkProgram(srcs); err != nil {
			return usage(err.Error())
		}
	} else {
		opt := buildOptions{out: r.Output, x: r.Options.Commands, work: r.Options.KeepWork, o0: r.Options.O0,
			progress: func(s step, done, total int) { p.Stage(buildStages[s], done, total) }}
		if err := checkOutput(opt.out, r.Files); err != nil {
			return usage(err.Error())
		}
		log := &lineWriter{emit: p.Log}
		ds, err = build(srcs, opt, log)
		log.flush()
		if err != nil {
			return usage(err.Error())
		}
	}
	diag.Sort(ds)
	return tui.Result{Diags: ds, Exit: diag.ExitCode(ds)}
}

// A lineWriter passes each complete line written to it to emit.
type lineWriter struct {
	emit func(string)
	buf  []byte
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		w.emit(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
}

// flush passes on the last line, if it has no newline.
func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}
