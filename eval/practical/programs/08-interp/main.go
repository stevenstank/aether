// Command mini runs a script in a small dynamically typed language with
// integers, strings, lists, maps, first-class functions and closures.
//
// Usage: mini [FILE]   (reads standard input without FILE)
//
// Exit status: 0 on success, 65 for a syntax error, 70 for a runtime
// error, 66 if the script cannot be read.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

func main() {
	var src []byte
	var err error
	if len(os.Args) > 1 {
		src, err = os.ReadFile(os.Args[1])
	} else {
		src, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mini:", err)
		os.Exit(66)
	}
	toks, err := Lex(string(src))
	if err == nil {
		var prog []Stmt
		prog, err = Parse(toks)
		if err == nil {
			out := bufio.NewWriter(os.Stdout)
			err = NewInterp(out).Run(prog)
			out.Flush()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		var e *Error
		if errors.As(err, &e) && e.Phase == "syntax" {
			os.Exit(65)
		}
		os.Exit(70)
	}
}
