// Command scheme is an interpreter for a Scheme subset with an exact
// numeric tower (fixnums, bignums, rationals), proper tail calls, escape
// continuations, and error handlers.
//
// Usage: scheme [-max-depth N] [-stats] [file ...]   (stdin when no file)
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

func (in *Interp) runSource(name, src string) (err error) {
	forms, err := readAll(src)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, f := range forms {
		if err := in.evalTop(f); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interp) evalTop(form Value) (err error) {
	defer func() {
		if r := recover(); r != nil {
			in.depth = 0
			switch e := r.(type) {
			case *SchemeError:
				err = e
			case *escape:
				err = errors.New("escape continuation invoked outside its extent")
			default:
				panic(r)
			}
		}
	}()
	in.Eval(form, in.global)
	return nil
}

func main() {
	maxDepth := flag.Int("max-depth", 100000, "maximum evaluation depth")
	stats := flag.Bool("stats", false, "print evaluation statistics to stderr")
	flag.Parse()

	w := bufio.NewWriter(os.Stdout)
	in := &Interp{global: NewEnv(nil), maxDepth: *maxDepth, out: func(s string) { w.WriteString(s) }}
	in.installBuiltins()
	if err := in.runSource("prelude", prelude); err != nil {
		panic(err)
	}
	debug.SetMaxStack(1 << 30)

	type source struct{ name, text string }
	var sources []source
	if flag.NArg() == 0 {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scheme:", err)
			os.Exit(1)
		}
		sources = append(sources, source{"<stdin>", string(b)})
	}
	for _, name := range flag.Args() {
		b, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scheme:", err)
			os.Exit(1)
		}
		sources = append(sources, source{name, string(b)})
	}
	status := 0
	for _, s := range sources {
		if err := in.runSource(s.name, s.text); err != nil {
			w.Flush()
			var re *ReadError
			if errors.As(err, &re) {
				fmt.Fprintln(os.Stderr, "scheme:", err)
				status = 2
			} else {
				fmt.Fprintln(os.Stderr, "error:", err)
				status = 1
			}
			break
		}
	}
	w.Flush()
	if *stats {
		fmt.Fprintf(os.Stderr, "steps: %d\n", in.steps)
	}
	os.Exit(status)
}
