package tui

import (
	"go/scanner"
	"go/token"
	"strings"
)

// A span colors bytes [start, end) of a line.
type span struct {
	start, end int
	st         Style
}

var builtins = map[string]bool{
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
	"bool": true, "byte": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true,
	"int16": true, "int32": true, "int64": true, "rune": true, "string": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "any": true, "comparable": true,
	"true": true, "false": true, "nil": true, "iota": true,
}

// highlight returns the colored spans of each line of Go source, from
// go/scanner's tokens. Source that does not scan is colored as far as it
// does; a token spanning lines, such as a raw string, is split at them.
func highlight(lines []string) [][]span {
	src := []byte(strings.Join(lines, "\n"))
	starts := make([]int, len(lines)) // byte offset of each line
	off := 0
	for i, l := range lines {
		starts[i] = off
		off += len(l) + 1
	}
	out := make([][]span, len(lines))
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, func(token.Position, string) {}, scanner.ScanComments)
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		var st Style
		switch {
		case tok == token.COMMENT:
			st = Style{Fg: cComment, Italic: true}
		case tok == token.STRING || tok == token.CHAR:
			st = Style{Fg: cString}
		case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
			st = Style{Fg: cNumber}
		case tok.IsKeyword():
			st = Style{Fg: cKeyword, Bold: true}
		case tok == token.IDENT && builtins[lit]:
			st = Style{Fg: cBuiltin}
		case tok == token.IDENT:
			st = Style{Fg: cIdent}
		default:
			continue
		}
		if tok == token.SEMICOLON || lit == "" {
			continue
		}
		start := file.Offset(pos)
		end := start + len(lit)
		if tok.IsKeyword() {
			end = start + len(tok.String())
		}
		// Split the token at line ends.
		line := file.Line(pos) - 1
		for start < end && line < len(lines) {
			lineEnd := starts[line] + len(lines[line])
			e := min(end, lineEnd)
			if e > start {
				out[line] = append(out[line], span{start - starts[line], e - starts[line], st})
			}
			start = lineEnd + 1
			line++
		}
	}
	return out
}
