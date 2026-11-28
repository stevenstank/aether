package tui

import (
	"strings"
	"unicode/utf8"
)

// KeyCode names a key that is not a printable character.
type KeyCode int

const (
	KeyRune KeyCode = iota // a printable character, in Key.Rune
	KeyEnter
	KeyEsc
	KeyTab
	KeyBacktab
	KeyBackspace
	KeyDelete
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyCtrl // a control key, Ctrl and the letter in Key.Rune
)

// A Key is one key press.
type Key struct {
	Code KeyCode
	Rune rune
}

func runeKey(r rune) Key { return Key{Code: KeyRune, Rune: r} }

// csiFinal are the keys of the sequences ESC [ X and ESC O X.
var csiFinal = map[byte]KeyCode{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft,
	'H': KeyHome, 'F': KeyEnd, 'Z': KeyBacktab,
}

// csiTilde are the keys of the sequences ESC [ N ~.
var csiTilde = map[string]KeyCode{
	"1": KeyHome, "7": KeyHome, "4": KeyEnd, "8": KeyEnd,
	"3": KeyDelete, "5": KeyPgUp, "6": KeyPgDn,
}

// parseKeys decodes the bytes of one read from the terminal. The terminal
// sends an escape sequence in one write, so an ESC that ends the input, or
// that no known sequence follows, is the Esc key. Unknown sequences are
// dropped whole.
func parseKeys(b []byte) []Key {
	var keys []Key
	for len(b) > 0 {
		c := b[0]
		switch {
		case c == 0x1b:
			k, n := parseEscape(b)
			if k.Code != KeyRune || k.Rune != 0 {
				keys = append(keys, k)
			}
			b = b[n:]
			continue
		case c == '\r' || c == '\n':
			keys = append(keys, Key{Code: KeyEnter})
		case c == '\t':
			keys = append(keys, Key{Code: KeyTab})
		case c == 0x7f || c == 0x08:
			keys = append(keys, Key{Code: KeyBackspace})
		case c < 0x20:
			keys = append(keys, Key{Code: KeyCtrl, Rune: rune('a' + c - 1)})
		default:
			r, n := utf8.DecodeRune(b)
			if r != utf8.RuneError || n > 1 {
				keys = append(keys, runeKey(r))
			}
			b = b[n:]
			continue
		}
		b = b[1:]
	}
	return keys
}

// parseEscape decodes the sequence at the start of b, which begins with ESC,
// and returns its key and length. The zero Key means the sequence is unknown.
func parseEscape(b []byte) (Key, int) {
	if len(b) == 1 || (b[1] != '[' && b[1] != 'O') {
		return Key{Code: KeyEsc}, 1
	}
	// CSI: parameter bytes 0x30-0x3f, then a final byte 0x40-0x7e.
	i := 2
	for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
		i++
	}
	if i == len(b) {
		return Key{Code: KeyEsc}, 1
	}
	final, params := b[i], string(b[2:i])
	n := i + 1
	if final == '~' {
		if semi := strings.IndexByte(params, ';'); semi >= 0 {
			params = params[:semi]
		}
		if code, ok := csiTilde[params]; ok {
			return Key{Code: code}, n
		}
		return Key{}, n
	}
	if code, ok := csiFinal[final]; ok {
		return Key{Code: code}, n
	}
	return Key{}, n
}
