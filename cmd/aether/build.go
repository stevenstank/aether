package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stevenstank/aether/internal/amd64"
	"github.com/stevenstank/aether/internal/diag"
	"github.com/stevenstank/aether/internal/toolchain"
)

// buildOptions are the command-line options of "aether build".
type buildOptions struct {
	out  string // -o: the output path
	x    bool   // -x: log every command and tool version to stderr
	work bool   // -work: keep the work directory and print its path
}

// errUsage reports a problem with the command line or the files it names;
// its message has already been printed.
var errUsage = errors.New("usage error")

func runBuild(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opt buildOptions
	fs.StringVar(&opt.out, "o", "", "")
	fs.BoolVar(&opt.x, "x", false, "")
	fs.BoolVar(&opt.work, "work", false, "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "aether build: %v\n", err)
		return diag.ExitUsage
	}
	srcs, err := readSources("build", fs.Args(), stderr)
	if err != nil {
		return diag.ExitUsage
	}
	if err := checkOutput(opt.out, fs.Args()); err != nil {
		fmt.Fprintf(stderr, "aether build: %v\n", err)
		return diag.ExitUsage
	}
	ds, err := build(srcs, opt, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "aether build: %v\n", err)
		return diag.ExitUsage
	}
	return report(stderr, ds)
}

// checkOutput validates the output path before anything is built. The
// output may replace an existing regular file, but never the input, a
// directory, a symbolic link, or any other kind of file.
func checkOutput(out string, inputs []string) error {
	if out == "" {
		return errors.New("no output file: use -o FILE")
	}
	fi, err := os.Lstat(out)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("output %s exists and is not a regular file", out)
	}
	for _, in := range inputs {
		if ii, err := os.Stat(in); err == nil && os.SameFile(fi, ii) {
			return fmt.Errorf("output %s is the input file", out)
		}
	}
	return nil
}

// build compiles srcs into the executable opt.out. Problems with the
// program or the tools are returned as diagnostics; failures of Aether's own
// file operations (the work directory, the output file) as an error. On any
// failure, opt.out is neither created nor modified.
func build(srcs []source, opt buildOptions, stderr io.Writer) ([]diag.Diagnostic, error) {
	var asm []byte
	ds := guard(func() []diag.Diagnostic {
		prog, ds := compile(srcs)
		if len(ds) > 0 {
			return ds
		}
		var err error
		if asm, err = amd64.Emit(prog); err != nil {
			return []diag.Diagnostic{{Class: diag.Internal, Pos: diag.Pos{File: srcs[0].name}, Message: err.Error()}}
		}
		return nil
	})
	if len(ds) > 0 {
		return ds, nil
	}
	var log io.Writer
	if opt.x {
		log = stderr
	}
	var tools [2]toolchain.Tool
	for i, name := range []string{"as", "ld"} {
		t, err := toolchain.Find(name, log)
		if err != nil {
			return toolDiag(err), nil
		}
		tools[i] = t
	}
	as, ld := tools[0], tools[1]

	work, err := os.MkdirTemp("", "aether-build-")
	if err != nil {
		return nil, err
	}
	if opt.work {
		fmt.Fprintf(stderr, "WORK=%s\n", work)
	} else {
		defer os.RemoveAll(work)
	}
	if log != nil {
		fmt.Fprintf(log, "cd %s\n", work)
	}
	// Fixed relative names in a fresh directory: the bytes produced do not
	// depend on where the directory is (D-008).
	files := []amd64.File{{Name: "prog.s", Data: asm}}
	files = append(files, amd64.Runtime()...)
	var objs []string
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(work, f.Name), f.Data, 0o644); err != nil {
			return nil, err
		}
		obj := strings.TrimSuffix(f.Name, ".s") + ".o"
		if err := as.Run(work, []string{"--64", "-o", obj, f.Name}, log); err != nil {
			return toolDiag(err), nil
		}
		objs = append(objs, obj)
	}
	if err := ld.Run(work, append(linkArgs, objs...), log); err != nil {
		return toolDiag(err), nil
	}
	if _, err := os.Stat(filepath.Join(work, linkOutput)); err != nil {
		return toolDiag(&toolchain.Error{Tool: ld.Name, Path: ld.Path, Version: ld.Version,
			Msg: "succeeded without writing " + linkOutput}), nil
	}
	return nil, publish(filepath.Join(work, linkOutput), opt.out)
}

// linkOutput is the linker's output in the work directory.
const linkOutput = "a.out"

// linkArgs precede the object files on the ld command line: a static
// executable entered at the runtime's _start, with no libraries.
var linkArgs = []string{"-static", "-nostdlib", "-e", "_start", "-o", linkOutput}

func toolDiag(err error) []diag.Diagnostic {
	return []diag.Diagnostic{{Class: diag.Tool, Message: err.Error()}}
}

// publish copies the linked executable to a temporary file next to out and
// renames it over out, so out is replaced only by a complete executable and
// is untouched if anything fails.
func publish(exe, out string) (err error) {
	data, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".aether-out-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Chmod(0o755); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), out)
}
