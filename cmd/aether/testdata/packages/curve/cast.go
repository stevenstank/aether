package curve

import "strings"

// Cast calls newHash with strings.NewReader.
func Cast() int {
	r := newHash(strings.NewReader)
	return r.Len() + PCT(C1())
}
