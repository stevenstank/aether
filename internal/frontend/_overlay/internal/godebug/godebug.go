// Package godebug is Aether's replacement of gc's internal/godebug (D-053):
// the settings of $GODEBUG, as gc's runtime gives them to its programs.
//
// A setting's value is its last name=value in the first GODEBUG of the
// environment, as gc's runtime reads it at startup, or "". gc's runtime
// also updates the values when the program sets GODEBUG with os.Setenv,
// counts IncNonDefault in runtime/metrics, and applies a value's
// "#pattern" suffix only to call stacks the pattern matches (bisect);
// Aether's values are those at startup, the counts are kept but not
// published, and a "#pattern" suffix is dropped, the value applying
// everywhere.
package godebug

import "internal/godebugs"

// A Setting is a single setting in the $GODEBUG environment variable.
type Setting struct {
	name       string
	looked     bool
	text       string
	info       *godebugs.Info
	registered bool
	nonDefault uint64
}

// New returns a new Setting for the $GODEBUG setting with the given name.
// A name not listed in internal/godebugs makes Value panic, unless it is
// prefixed with '#', as gc's does.
func New(name string) *Setting {
	return &Setting{name: name}
}

// Name returns the name of the setting.
func (s *Setting) Name() string {
	if s.name != "" && s.name[0] == '#' {
		return s.name[1:]
	}
	return s.name
}

// Undocumented reports whether this is an undocumented setting.
func (s *Setting) Undocumented() bool {
	return s.name != "" && s.name[0] == '#'
}

// String returns a printable form for the setting: name=value.
func (s *Setting) String() string {
	return s.Name() + "=" + s.Value()
}

// IncNonDefault increments the non-default behavior counter of the
// setting. Value must be called at least once before it.
func (s *Setting) IncNonDefault() {
	if !s.registered {
		if s.info == nil || s.info.Opaque {
			panic("godebug: unexpected IncNonDefault of " + s.name)
		}
		s.registered = true
	}
	s.nonDefault++
}

// Value returns the value of the setting.
func (s *Setting) Value() string {
	if !s.looked {
		s.looked = true
		s.info = godebugs.Lookup(s.Name())
		if s.info == nil && !s.Undocumented() {
			panic("godebug: Value of name not listed in godebugs.All: " + s.name)
		}
		s.text = lookup(s.Name())
	}
	return s.text
}

// envc and env are the program's environment strings, as os's: intrinsics.
func envc() int
func env(i int) string

// lookup returns the value of name in GODEBUG: of its last name=value, as
// gc's parse scans backward, without a "#pattern" suffix.
func lookup(name string) string {
	const prefix = "GODEBUG="
	var d string
	for i := 0; i < envc(); i++ {
		if s := env(i); len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			d = s[len(prefix):]
			break
		}
	}
	end, eq := len(d), -1
	for i := len(d) - 1; i >= -1; i-- {
		if i == -1 || d[i] == ',' {
			if eq >= 0 && d[i+1:eq] == name {
				arg := d[eq+1 : end]
				for j := 0; j < len(arg); j++ {
					if arg[j] == '#' {
						return arg[:j]
					}
				}
				return arg
			}
			eq, end = -1, i
		} else if d[i] == '=' {
			eq = i
		}
	}
	return ""
}
