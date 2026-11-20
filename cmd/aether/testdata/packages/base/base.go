// Package base is imported by every other package of the module.
package base

// Log records the order of initialization.
var Log []string

func Note(s string) { Log = append(Log, s) }

const Max = 10

var Count = start()

func start() int {
	Note("base.var")
	return 3
}

func init() { Note("base.init") }

// Kind has the same name as alpha.Kind, and so do their methods.
type Kind int

func (k Kind) String() string { return "base kind" }
