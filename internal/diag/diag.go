// Package diag defines Aether's diagnostics: their classes, source positions,
// deterministic rendering, and the CLI exit codes derived from them
// (docs/ARCHITECTURE.md §Diagnostics, D-003).
//
// The package has no dependencies on go/* packages, so any stage, including
// those that must not see go/ast or go/types, can report diagnostics.
package diag

import (
	"fmt"
	"sort"
	"strings"
)

// Class is the category of a diagnostic. Every diagnostic has exactly one.
type Class int

const (
	// Invalid means the program is not valid Go.
	Invalid Class = iota + 1
	// Unsupported means the program is valid Go but outside Aether's
	// supported subset.
	Unsupported
	// Internal means Aether itself malfunctioned.
	Internal
	// Tool means a required external tool is missing, unidentified, or
	// failed.
	Tool
)

func (c Class) String() string {
	switch c {
	case Invalid:
		return "invalid"
	case Unsupported:
		return "unsupported"
	case Internal:
		return "internal"
	case Tool:
		return "tool"
	}
	return fmt.Sprintf("Class(%d)", int(c))
}

// Exit codes of the aether command (finalized in Phase 1; D-018).
const (
	ExitOK          = 0
	ExitInvalid     = 1
	ExitUsage       = 2
	ExitUnsupported = 3
	ExitInternal    = 4
	ExitTool        = 5
)

// Pos is a source position. Line and Col are 1-based. A Pos with Line 0
// refers to a whole file; a Pos with an empty File refers to no file.
type Pos struct {
	File string
	Line int
	Col  int
}

func (p Pos) String() string {
	switch {
	case p.File == "":
		return ""
	case p.Line == 0:
		return p.File
	case p.Col == 0:
		return fmt.Sprintf("%s:%d", p.File, p.Line)
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// Diagnostic is one message about the program being compiled.
type Diagnostic struct {
	Class   Class
	Pos     Pos
	Message string
	// Feature names the unsupported construct, using the names listed in
	// docs/SEMANTICS.md. It is set only for Unsupported diagnostics.
	Feature string
}

// String renders d as "file:line:col: class: message", followed by
// " [feature]" for unsupported diagnostics. The message is kept on one line.
func (d Diagnostic) String() string {
	var b strings.Builder
	if pos := d.Pos.String(); pos != "" {
		b.WriteString(pos)
		b.WriteString(": ")
	}
	b.WriteString(d.Class.String())
	b.WriteString(": ")
	b.WriteString(strings.ReplaceAll(d.Message, "\n", " "))
	if d.Feature != "" {
		b.WriteString(" [")
		b.WriteString(d.Feature)
		b.WriteString("]")
	}
	return b.String()
}

// Sort orders diagnostics by file, line, column, class, feature, and message,
// so that rendering never depends on the order in which they were found.
func Sort(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Pos.File != b.Pos.File {
			return a.Pos.File < b.Pos.File
		}
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line < b.Pos.Line
		}
		if a.Pos.Col != b.Pos.Col {
			return a.Pos.Col < b.Pos.Col
		}
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		if a.Feature != b.Feature {
			return a.Feature < b.Feature
		}
		return a.Message < b.Message
	})
}

// ExitCode returns the exit code for a completed run that produced ds.
// An internal error dominates every other class, because the other results
// cannot be trusted once Aether has malfunctioned; then tool, invalid, and
// unsupported, in that order.
func ExitCode(ds []Diagnostic) int {
	has := map[Class]bool{}
	for _, d := range ds {
		has[d.Class] = true
	}
	switch {
	case has[Internal]:
		return ExitInternal
	case has[Tool]:
		return ExitTool
	case has[Invalid]:
		return ExitInvalid
	case has[Unsupported]:
		return ExitUnsupported
	}
	return ExitOK
}
