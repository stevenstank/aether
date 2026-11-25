package main

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version (major.minor.patch[-pre]).
type Version struct {
	Major, Minor, Patch int
	Pre                 []string
}

var ErrBadVersion = errors.New("invalid version")

func ParseVersion(s string) (Version, error) {
	orig := s
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata is ignored
	}
	var v Version
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("%w %q: want major.minor.patch", ErrBadVersion, orig)
	}
	nums := [3]*int{&v.Major, &v.Minor, &v.Patch}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, fmt.Errorf("%w %q: bad number %q", ErrBadVersion, orig, p)
		}
		*nums[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, fmt.Errorf("%w %q: empty prerelease", ErrBadVersion, orig)
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		ai, aerr := strconv.Atoi(a[i])
		bi, berr := strconv.Atoi(b[i])
		var c int
		switch {
		case aerr == nil && berr == nil:
			c = cmp.Compare(ai, bi)
		case aerr == nil:
			c = -1
		case berr == nil:
			c = 1
		default:
			c = strings.Compare(a[i], b[i])
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func (v Version) Compare(o Version) int {
	if c := cmp.Compare(v.Major, o.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Minor, o.Minor); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Patch, o.Patch); c != 0 {
		return c
	}
	return comparePre(v.Pre, o.Pre)
}

type op int

const (
	opEQ op = iota
	opGT
	opGE
	opLT
	opLE
)

type comparator struct {
	op op
	v  Version
}

func (c comparator) allows(v Version) bool {
	d := v.Compare(c.v)
	switch c.op {
	case opEQ:
		return d == 0
	case opGT:
		return d > 0
	case opGE:
		return d >= 0
	case opLT:
		return d < 0
	}
	return d <= 0
}

// Constraint is a disjunction of conjunctions, as in "^1.2 || >=3.0.0 <3.5.0".
type Constraint struct {
	text string
	sets [][]comparator
}

func (c Constraint) String() string { return c.text }

func (c Constraint) Allows(v Version) bool {
	for _, set := range c.sets {
		ok := true
		hasPre := false
		for _, cmp := range set {
			if !cmp.allows(v) {
				ok = false
				break
			}
			if len(cmp.v.Pre) > 0 && cmp.v.Major == v.Major && cmp.v.Minor == v.Minor && cmp.v.Patch == v.Patch {
				hasPre = true
			}
		}
		// Prereleases only match when a comparator names the same core version.
		if ok && (len(v.Pre) == 0 || hasPre) {
			return true
		}
	}
	return false
}

// partial parses "1", "1.2", "1.2.3", or with x/* wildcards; n is the
// number of fixed components.
func partial(s string) (Version, int, error) {
	s = strings.TrimPrefix(s, "v")
	if s == "*" || s == "x" || s == "X" || s == "" {
		return Version{}, 0, nil
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return Version{}, 0, fmt.Errorf("%w %q", ErrBadVersion, s)
	}
	var v Version
	nums := [3]*int{&v.Major, &v.Minor, &v.Patch}
	n := 0
	for i, p := range parts {
		if p == "x" || p == "X" || p == "*" {
			break
		}
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return Version{}, 0, fmt.Errorf("%w %q", ErrBadVersion, s)
		}
		*nums[i] = x
		n++
	}
	if hasPre {
		if n < 3 {
			return Version{}, 0, fmt.Errorf("%w %q: prerelease needs a full version", ErrBadVersion, s)
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, n, nil
}

func bump(v Version, n int) Version {
	switch n {
	case 0:
		return Version{Major: v.Major + 1}
	case 1:
		return Version{Major: v.Major, Minor: v.Minor + 1}
	}
	return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}
}

func ParseConstraint(s string) (Constraint, error) {
	c := Constraint{text: strings.TrimSpace(s)}
	for _, alt := range strings.Split(s, "||") {
		fields := strings.Fields(alt)
		if len(fields) == 0 {
			return c, fmt.Errorf("empty constraint in %q", s)
		}
		var set []comparator
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			// Hyphen range: "1.2 - 2.3"
			if i+2 < len(fields) && fields[i+1] == "-" {
				lo, _, err := partial(f)
				if err != nil {
					return c, err
				}
				hi, n, err := partial(fields[i+2])
				if err != nil {
					return c, err
				}
				set = append(set, comparator{opGE, lo})
				if n == 3 {
					set = append(set, comparator{opLE, hi})
				} else {
					set = append(set, comparator{opLT, bump(hi, n-1)})
				}
				i += 2
				continue
			}
			cs, err := parseOne(f)
			if err != nil {
				return c, fmt.Errorf("constraint %q: %w", s, err)
			}
			set = append(set, cs...)
		}
		c.sets = append(c.sets, set)
	}
	return c, nil
}

func parseOne(f string) ([]comparator, error) {
	prefixes := []struct {
		p  string
		op op
	}{{">=", opGE}, {"<=", opLE}, {">", opGT}, {"<", opLT}, {"=", opEQ}}
	for _, pr := range prefixes {
		if strings.HasPrefix(f, pr.p) {
			v, n, err := partial(f[len(pr.p):])
			if err != nil {
				return nil, err
			}
			if n == 3 || pr.op == opGE || pr.op == opLT {
				return []comparator{{pr.op, v}}, nil
			}
			// Partial versions: ">1.2" means ">=1.3.0", "<=1.2" means "<1.3.0".
			switch pr.op {
			case opGT:
				if n == 0 {
					return []comparator{{opLT, Version{}}}, nil // matches nothing
				}
				return []comparator{{opGE, bump(v, n-1)}}, nil
			case opLE:
				if n == 0 {
					return []comparator{{opGE, Version{}}}, nil
				}
				return []comparator{{opLT, bump(v, n-1)}}, nil
			}
			return xrange(v, n), nil
		}
	}
	switch f[0] {
	case '^':
		v, n, err := partial(f[1:])
		if err != nil {
			return nil, err
		}
		upper := Version{Major: v.Major + 1}
		switch {
		case v.Major == 0 && (n >= 2 && (v.Minor > 0 || n == 2)):
			upper = Version{Minor: v.Minor + 1}
		case v.Major == 0 && v.Minor == 0 && n == 3:
			upper = Version{Patch: v.Patch + 1}
		case v.Major == 0 && n == 1:
			upper = Version{Major: 1}
		}
		return []comparator{{opGE, v}, {opLT, upper}}, nil
	case '~':
		v, n, err := partial(strings.TrimPrefix(f[1:], ">"))
		if err != nil {
			return nil, err
		}
		if n <= 1 {
			return []comparator{{opGE, v}, {opLT, Version{Major: v.Major + 1}}}, nil
		}
		return []comparator{{opGE, v}, {opLT, Version{Major: v.Major, Minor: v.Minor + 1}}}, nil
	}
	v, n, err := partial(f)
	if err != nil {
		return nil, err
	}
	return xrange(v, n), nil
}

func xrange(v Version, n int) []comparator {
	if n == 3 {
		return []comparator{{opEQ, v}}
	}
	if n == 0 {
		return []comparator{{opGE, Version{}}}
	}
	return []comparator{{opGE, v}, {opLT, bump(v, n-1)}}
}
