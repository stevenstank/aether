//go:build !linux

package tui

import (
	"errors"
	"os"
)

// Aether targets Linux (D-007); elsewhere the TUI reports that it has no
// terminal support, and the rest of the driver still builds.

type rawTerm struct{}

func isTerminal(*os.File) bool { return false }

func makeRaw(*os.File) (*rawTerm, error) { return nil, errors.New("raw mode needs Linux") }

func (*rawTerm) restore() {}

func termSize(*os.File) (int, int, error) { return 0, 0, errors.New("terminal size needs Linux") }

var resizeSignal os.Signal // none: Run never starts without a terminal

var quitSignals []os.Signal
