package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"
)

// ErrNoTerminal reports that the TUI cannot run here: standard input or
// output is not a terminal, or the terminal cannot position the cursor.
var ErrNoTerminal = errors.New("not an interactive terminal")

var homeDir, _ = os.UserHomeDir()

// Terminal modes the TUI sets, and the sequences that undo them: the
// alternate screen, a hidden cursor, and no automatic wrap at the margin.
const (
	enterSeq = "\x1b[?1049h\x1b[?25l\x1b[?7l"
	leaveSeq = "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l"
)

// colorMode chooses the color mode from the environment: NO_COLOR turns
// colors off (no-color.org), COLORTERM announces 24-bit color, and the
// Linux console has only 16 colors, which are left alone.
func colorMode(getenv func(string) string) ColorMode {
	switch {
	case getenv("NO_COLOR") != "":
		return ColorNone
	case getenv("COLORTERM") == "truecolor" || getenv("COLORTERM") == "24bit":
		return ColorTrue
	case getenv("TERM") == "linux":
		return ColorNone
	}
	return Color256
}

// supported reports whether the terminal type TERM can be drawn on.
func supported(term string) bool {
	return term != "" && term != "dumb"
}

// Run runs the TUI on the process's terminal until the user quits. It
// returns ErrNoTerminal, wrapped, if there is no usable terminal, and
// always restores the terminal, also when the TUI panics.
func Run(cfg Config) error {
	in, out := os.Stdin, os.Stdout
	if !isTerminal(in) || !isTerminal(out) {
		return fmt.Errorf("%w: standard input and output must be a terminal", ErrNoTerminal)
	}
	if term := os.Getenv("TERM"); !supported(term) {
		return fmt.Errorf("%w: TERM=%q cannot move the cursor", ErrNoTerminal, term)
	}
	w, h, err := termSize(out)
	if err != nil || w == 0 || h == 0 {
		return fmt.Errorf("%w: unknown terminal size", ErrNoTerminal)
	}
	raw, err := makeRaw(in)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoTerminal, err)
	}
	io.WriteString(out, enterSeq)
	defer func() {
		io.WriteString(out, leaveSeq)
		raw.restore()
	}()

	m := newModel(cfg)
	m.update(resizeMsg{w, h})

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, append([]os.Signal{resizeSignal}, quitSignals...)...)
	defer signal.Stop(sigs)
	go func() {
		for sig := range sigs {
			if sig == resizeSignal {
				if w, h, err := termSize(out); err == nil {
					m.events <- resizeMsg{w, h}
				}
				continue
			}
			m.events <- quitMsg{}
		}
	}()
	go readKeys(in, m.events)

	return loop(m, out, colorMode(os.Getenv))
}

// readKeys sends the keys read from in until it fails.
func readKeys(in io.Reader, events chan<- any) {
	buf := make([]byte, 256)
	for {
		n, err := in.Read(buf)
		for _, k := range parseKeys(buf[:n]) {
			events <- keyMsg(k)
		}
		if err != nil {
			events <- quitMsg{}
			return
		}
	}
}

// loop applies events and redraws until the model quits. It applies every
// pending event before it draws, so a burst of output costs one frame.
func loop(m *model, out io.Writer, mode ColorMode) error {
	p := &painter{mode: mode}
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	for !m.quit {
		if _, err := io.WriteString(out, p.frame(m.view())); err != nil {
			return err
		}
		ev := waitEvent(m, tick.C)
		for {
			if k, ok := ev.(keyMsg); ok && Key(k) == (Key{Code: KeyCtrl, Rune: 'l'}) {
				p.prev = nil // ctrl+l: redraw everything
			} else if _, ok := ev.(resizeMsg); ok {
				p.prev = nil
				m.update(ev)
			} else {
				m.update(ev)
			}
			select {
			case ev = <-m.events:
				continue
			default:
			}
			break
		}
	}
	return nil
}

// waitEvent returns the next event; the ticker animates only while a run is
// in progress or its outcome is fresh, so an idle TUI uses no CPU.
func waitEvent(m *model, tick <-chan time.Time) any {
	for {
		select {
		case ev := <-m.events:
			return ev
		case <-tick:
			if m.running {
				return tickMsg{}
			}
		}
	}
}
