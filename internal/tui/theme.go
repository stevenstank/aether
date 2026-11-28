package tui

import "github.com/stevenstank/aether/internal/diag"

// The palette: a dark theme with a violet accent. Panels keep the
// terminal's own background; only the bars and selections set one.
var (
	cAccent  = RGB(0xa7, 0x8b, 0xfa) // violet
	cAccent2 = RGB(0x67, 0xd4, 0xe0) // cyan
	cText    = RGB(0xe4, 0xe4, 0xe7)
	cMuted   = RGB(0x8b, 0x8b, 0x96)
	cFaint   = RGB(0x5a, 0x5a, 0x66)
	cBorder  = RGB(0x3f, 0x3f, 0x4a)
	cBar     = RGB(0x1c, 0x1c, 0x24)
	cBarHi   = RGB(0x2c, 0x2c, 0x3a)
	cSel     = RGB(0x2e, 0x2a, 0x48)
	cSelDim  = RGB(0x26, 0x26, 0x30)
	cOK      = RGB(0x86, 0xd9, 0x8a)
	cErr     = RGB(0xf8, 0x71, 0x71)
	cWarn    = RGB(0xfa, 0xcc, 0x6b)
	cTool    = RGB(0xfb, 0x9a, 0x5c)
	cInt     = RGB(0xf0, 0x8b, 0xd8)

	// Syntax colors.
	cKeyword = RGB(0xc4, 0xa7, 0xff)
	cString  = RGB(0xa5, 0xd6, 0x8f)
	cNumber  = RGB(0xf5, 0xb3, 0x7e)
	cComment = RGB(0x6b, 0x6b, 0x7a)
	cIdent   = RGB(0xd4, 0xd4, 0xd8)
	cBuiltin = RGB(0x7d, 0xcf, 0xe3)
)

var (
	stText    = Style{Fg: cText}
	stMuted   = Style{Fg: cMuted}
	stFaint   = Style{Fg: cFaint}
	stBorder  = Style{Fg: cBorder}
	stFocus   = Style{Fg: cAccent}
	stTitle   = Style{Fg: cMuted, Bold: true}
	stTitleOn = Style{Fg: cAccent, Bold: true}
	stBar     = Style{Fg: cMuted, Bg: cBar}
	stKey     = Style{Fg: cAccent2, Bg: cBar, Bold: true}
	stOK      = Style{Fg: cOK, Bold: true}
	stErr     = Style{Fg: cErr, Bold: true}
	stWarn    = Style{Fg: cWarn, Bold: true}
)

// classStyle and classIcon present a diagnostic class.
func classStyle(c diag.Class) Style {
	switch c {
	case diag.Invalid:
		return Style{Fg: cErr, Bold: true}
	case diag.Unsupported:
		return Style{Fg: cWarn, Bold: true}
	case diag.Tool:
		return Style{Fg: cTool, Bold: true}
	}
	return Style{Fg: cInt, Bold: true}
}

func classIcon(c diag.Class) rune {
	switch c {
	case diag.Invalid:
		return '✗'
	case diag.Unsupported:
		return '◆'
	case diag.Tool:
		return '⚙'
	}
	return '‼'
}

// withBg returns st on background bg.
func withBg(st Style, bg Color) Style {
	st.Bg = bg
	return st
}

// exitMeaning describes the exit status of aether check and build.
func exitMeaning(code int) string {
	switch code {
	case diag.ExitOK:
		return "success"
	case diag.ExitInvalid:
		return "invalid Go"
	case diag.ExitUsage:
		return "usage or file error"
	case diag.ExitUnsupported:
		return "outside the supported subset"
	case diag.ExitInternal:
		return "internal compiler error"
	case diag.ExitTool:
		return "GNU as or ld failed"
	}
	return "unknown status"
}
