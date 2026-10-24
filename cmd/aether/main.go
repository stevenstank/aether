// Command aether is the driver for the Aether compiler.
//
// "aether check" classifies a Go source file as invalid Go, valid Go outside
// Aether's supported subset (S1), or acceptable. "aether build" compiles an
// S1 program into a static Linux x86-64 executable with Aether's own backend
// and runtime, using GNU as and ld only to assemble and link (D-007, D-008).
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/stevenstank/aether/internal/diag"
)

const usage = `usage: aether <command> [arguments]

Commands:
  check FILE.go                     report whether FILE.go is inside Aether's
                                    supported subset (S1)
  build [-x] [-work] -o OUT FILE.go compile FILE.go into the executable OUT
  help                              print this message

Build flags:
  -o OUT   output file; an existing regular file is replaced only on success
  -x       print the commands run and the tool versions to stderr
  -work    keep the temporary work directory and print its path

Exit status of check and build:
  0  success
  1  invalid Go
  2  usage error, or the work directory or output cannot be written
  3  valid Go outside the supported subset
  4  internal compiler error
  5  GNU as or ld missing, not identified, or failed
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command line args and returns the exit status.
// Diagnostics and usage errors go to stderr; only "help" writes to stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return diag.ExitUsage
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return diag.ExitOK
	case "check":
		return runCheck(args[1:], stderr)
	case "build":
		return runBuild(args[1:], stderr)
	}
	fmt.Fprintf(stderr, "aether: unknown command %q\n\n%s", args[0], usage)
	return diag.ExitUsage
}

func runCheck(args []string, stderr io.Writer) int {
	srcs, err := readSources("check", args, stderr)
	if err != nil {
		return diag.ExitUsage
	}
	return report(stderr, guard(func() []diag.Diagnostic { return check(srcs) }))
}

// readSources reads the source files named on the command line of cmd. A
// problem is reported to stderr and returned as errUsage.
func readSources(cmd string, names []string, stderr io.Writer) ([]source, error) {
	if len(names) == 0 {
		fmt.Fprintf(stderr, "aether %s: no input file\n", cmd)
		return nil, errUsage
	}
	var srcs []source
	for _, name := range names {
		if strings.HasPrefix(name, "-") {
			fmt.Fprintf(stderr, "aether %s: unknown flag %s\n", cmd, name)
			return nil, errUsage
		}
		if !strings.HasSuffix(name, ".go") {
			fmt.Fprintf(stderr, "aether %s: %s is not a .go file\n", cmd, name)
			return nil, errUsage
		}
		data, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(stderr, "aether %s: %v\n", cmd, err)
			return nil, errUsage
		}
		srcs = append(srcs, source{name: name, data: data})
	}
	return srcs, nil
}

// report writes ds to w, one per line, and returns the exit status.
func report(w io.Writer, ds []diag.Diagnostic) int {
	diag.Sort(ds)
	for _, d := range ds {
		fmt.Fprintln(w, d)
	}
	return diag.ExitCode(ds)
}

// guard runs f and converts a panic into an internal diagnostic, so that a
// crash inside Aether is never reported as a problem with the user's program.
func guard(f func() []diag.Diagnostic) (ds []diag.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			ds = append(ds, diag.Diagnostic{
				Class:   diag.Internal,
				Message: fmt.Sprintf("internal compiler error: %v", r),
			})
		}
	}()
	return f()
}
