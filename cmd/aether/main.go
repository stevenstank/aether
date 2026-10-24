// Command aether is the driver for the Aether compiler.
//
// Phase 1 provides only "aether check", which classifies a Go source file as
// invalid Go, valid Go outside Aether's supported subset (S1), or acceptable.
// It does not compile anything.
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
  check FILE.go   report whether FILE.go is inside Aether's supported subset (S1)
  help            print this message

Exit status of check:
  0  accepted
  1  invalid Go
  2  usage error
  3  valid Go outside the supported subset
  4  internal compiler error
  5  required external tool missing or failed
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
	}
	fmt.Fprintf(stderr, "aether: unknown command %q\n\n%s", args[0], usage)
	return diag.ExitUsage
}

func runCheck(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, "aether check: no input file\n")
		return diag.ExitUsage
	}
	var srcs []source
	for _, name := range args {
		if strings.HasPrefix(name, "-") {
			fmt.Fprintf(stderr, "aether check: unknown flag %s\n", name)
			return diag.ExitUsage
		}
		if !strings.HasSuffix(name, ".go") {
			fmt.Fprintf(stderr, "aether check: %s is not a .go file\n", name)
			return diag.ExitUsage
		}
		data, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(stderr, "aether check: %v\n", err)
			return diag.ExitUsage
		}
		srcs = append(srcs, source{name: name, data: data})
	}
	return report(stderr, guard(func() []diag.Diagnostic { return check(srcs) }))
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
