// Package proto parses command lines and formats replies.
package proto

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Command is a parsed request line.
type Command struct {
	Client string
	Name   string
	Args   []string
}

var ErrSyntax = errors.New("syntax error")

// Parse splits "client: NAME arg ..." into a Command. Arguments may be
// double-quoted with Go escapes. A line without "client:" belongs to
// client "main".
func Parse(line string) (Command, error) {
	cmd := Command{Client: "main"}
	if c, rest, ok := strings.Cut(line, ":"); ok && !strings.ContainsAny(c, " \t\"") && c != "" {
		cmd.Client = c
		line = rest
	}
	var words []string
	s := strings.TrimSpace(line)
	for s != "" {
		if s[0] == '"' {
			q, err := strconv.QuotedPrefix(s)
			if err != nil {
				return cmd, fmt.Errorf("%w: unterminated or bad quoted string", ErrSyntax)
			}
			u, _ := strconv.Unquote(q)
			words = append(words, u)
			s = s[len(q):]
			if s != "" && s[0] != ' ' && s[0] != '\t' {
				return cmd, fmt.Errorf("%w: text after closing quote", ErrSyntax)
			}
		} else {
			i := strings.IndexAny(s, " \t")
			if i < 0 {
				i = len(s)
			}
			words = append(words, s[:i])
			s = s[i:]
		}
		s = strings.TrimLeft(s, " \t")
	}
	if len(words) == 0 {
		return cmd, fmt.Errorf("%w: empty command", ErrSyntax)
	}
	cmd.Name = strings.ToUpper(words[0])
	cmd.Args = words[1:]
	return cmd, nil
}

// Reply is a response to a command.
type Reply struct {
	Kind  ReplyKind
	Str   string
	Int   int64
	Items []string
}

type ReplyKind int

const (
	OK ReplyKind = iota
	Nil
	Str
	Int
	List
	Err
)

func Format(r Reply) string {
	switch r.Kind {
	case OK:
		return "OK"
	case Nil:
		return "(nil)"
	case Str:
		return strconv.Quote(r.Str)
	case Int:
		return "(integer) " + strconv.FormatInt(r.Int, 10)
	case List:
		if len(r.Items) == 0 {
			return "(empty list)"
		}
		var b strings.Builder
		for i, it := range r.Items {
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "%d) %q", i+1, it)
		}
		return b.String()
	case Err:
		return "(error) " + r.Str
	}
	return "?"
}
