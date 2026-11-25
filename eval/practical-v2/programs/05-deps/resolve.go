package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Registry maps package names to their published releases.
type Registry struct {
	Packages map[string][]*Release
}

type Release struct {
	Name    string
	Version Version
	Deps    map[string]Constraint
	Yanked  bool
}

type ConflictError struct {
	Package string
	Demands []string // "who requires what"
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("no version of %s satisfies all requirements:\n  %s", e.Package, strings.Join(e.Demands, "\n  "))
}

var ErrUnknownPackage = errors.New("unknown package")

type requirement struct {
	from       string // "root" or "name@version"
	constraint Constraint
}

// Set is a small generic ordered-insertion set.
type Set[T comparable] struct {
	items []T
	index map[T]int
}

func (s *Set[T]) Add(v T) bool {
	if s.index == nil {
		s.index = map[T]int{}
	}
	if _, ok := s.index[v]; ok {
		return false
	}
	s.index[v] = len(s.items)
	s.items = append(s.items, v)
	return true
}

func (s *Set[T]) Has(v T) bool { _, ok := s.index[v]; return ok }
func (s *Set[T]) Len() int     { return len(s.items) }
func (s *Set[T]) Items() []T   { return slices.Clone(s.items) }

// Resolver performs a depth-first backtracking search, preferring the
// newest acceptable release of each package.
type Resolver struct {
	reg      *Registry
	steps    int
	maxSteps int
	trace    func(string)
}

type state struct {
	chosen map[string]*Release
	reqs   map[string][]requirement
}

func (s *state) clone() *state {
	n := &state{chosen: maps.Clone(s.chosen), reqs: make(map[string][]requirement, len(s.reqs))}
	for k, v := range s.reqs {
		n.reqs[k] = slices.Clone(v)
	}
	return n
}

func (r *Resolver) candidates(name string, reqs []requirement) []*Release {
	var out []*Release
	for _, rel := range r.reg.Packages[name] {
		if rel.Yanked {
			continue
		}
		ok := true
		for _, q := range reqs {
			if !q.constraint.Allows(rel.Version) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, rel)
		}
	}
	slices.SortFunc(out, func(a, b *Release) int { return b.Version.Compare(a.Version) })
	return out
}

// Resolve returns the chosen release of every package reachable from the
// root requirements.
func (r *Resolver) Resolve(root map[string]Constraint) (map[string]*Release, error) {
	st := &state{chosen: map[string]*Release{}, reqs: map[string][]requirement{}}
	for _, name := range slices.Sorted(maps.Keys(root)) {
		if _, ok := r.reg.Packages[name]; !ok {
			return nil, fmt.Errorf("root requires %s: %w", name, ErrUnknownPackage)
		}
		st.reqs[name] = append(st.reqs[name], requirement{"root", root[name]})
	}
	res, err := r.search(st, 0)
	if err != nil {
		return nil, err
	}
	return res.chosen, nil
}

// next picks the unresolved package with the fewest candidates (fail first),
// ties broken by name.
func (r *Resolver) next(st *state) (string, []*Release) {
	best := ""
	var bestC []*Release
	for _, name := range slices.Sorted(maps.Keys(st.reqs)) {
		if _, done := st.chosen[name]; done {
			continue
		}
		c := r.candidates(name, st.reqs[name])
		if best == "" || len(c) < len(bestC) {
			best, bestC = name, c
		}
	}
	return best, bestC
}

func (r *Resolver) search(st *state, depth int) (*state, error) {
	r.steps++
	if r.steps > r.maxSteps {
		return nil, fmt.Errorf("resolution gave up after %d steps", r.maxSteps)
	}
	name, cands := r.next(st)
	if name == "" {
		return st, nil
	}
	if len(cands) == 0 {
		ce := &ConflictError{Package: name}
		for _, q := range st.reqs[name] {
			ce.Demands = append(ce.Demands, fmt.Sprintf("%s requires %s %s", q.from, name, q.constraint))
		}
		slices.Sort(ce.Demands)
		return nil, ce
	}
	var firstErr error
	for _, rel := range cands {
		if r.trace != nil {
			r.trace(fmt.Sprintf("%strying %s@%s", strings.Repeat("  ", depth), name, rel.Version))
		}
		next := st.clone()
		next.chosen[name] = rel
		ok := true
		for _, dep := range slices.Sorted(maps.Keys(rel.Deps)) {
			if _, known := r.reg.Packages[dep]; !known {
				ok = false
				if firstErr == nil {
					firstErr = fmt.Errorf("%s@%s requires %s: %w", name, rel.Version, dep, ErrUnknownPackage)
				}
				break
			}
			q := requirement{from: name + "@" + rel.Version.String(), constraint: rel.Deps[dep]}
			if prev, done := next.chosen[dep]; done && !q.constraint.Allows(prev.Version) {
				ok = false
				if firstErr == nil {
					firstErr = &ConflictError{Package: dep, Demands: []string{
						fmt.Sprintf("%s requires %s %s", q.from, dep, q.constraint),
						fmt.Sprintf("but %s@%s was already selected", dep, prev.Version),
					}}
				}
				break
			}
			next.reqs[dep] = append(next.reqs[dep], q)
		}
		if !ok {
			continue
		}
		res, err := r.search(next, depth+1)
		if err == nil {
			return res, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if r.steps > r.maxSteps {
			return nil, err
		}
	}
	return nil, firstErr
}

// InstallOrder returns packages so that dependencies come before their
// dependents (Kahn's algorithm, alphabetical among ready packages). It
// reports a cycle if one exists.
func InstallOrder(chosen map[string]*Release) ([]string, error) {
	indeg := map[string]int{}
	users := map[string][]string{}
	for name, rel := range chosen {
		if _, ok := indeg[name]; !ok {
			indeg[name] = 0
		}
		for dep := range rel.Deps {
			indeg[name]++
			users[dep] = append(users[dep], name)
		}
	}
	var ready, order []string
	for name, d := range indeg {
		if d == 0 {
			ready = append(ready, name)
		}
	}
	slices.Sort(ready)
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		for _, u := range users[n] {
			indeg[u]--
			if indeg[u] == 0 {
				i, _ := slices.BinarySearch(ready, u)
				ready = slices.Insert(ready, i, u)
			}
		}
	}
	if len(order) != len(chosen) {
		return order, fmt.Errorf("dependency cycle: %s", strings.Join(findCycle(chosen), " -> "))
	}
	return order, nil
}

func findCycle(chosen map[string]*Release) []string {
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	var cycle []string
	var visit func(n string) bool
	visit = func(n string) bool {
		color[n] = grey
		stack = append(stack, n)
		for _, d := range slices.Sorted(maps.Keys(chosen[n].Deps)) {
			switch color[d] {
			case grey:
				i := slices.Index(stack, d)
				cycle = append(slices.Clone(stack[i:]), d)
				return true
			case white:
				if visit(d) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	for _, n := range slices.Sorted(maps.Keys(chosen)) {
		if color[n] == white && visit(n) {
			return cycle
		}
	}
	return nil
}

// Why returns the shortest dependency chain from the root to target.
func Why(root map[string]Constraint, chosen map[string]*Release, target string) []string {
	type item struct {
		name string
		path []string
	}
	var queue []item
	seen := Set[string]{}
	for _, n := range slices.Sorted(maps.Keys(root)) {
		queue = append(queue, item{n, []string{"root", n}})
		seen.Add(n)
	}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.name == target {
			return it.path
		}
		rel := chosen[it.name]
		if rel == nil {
			continue
		}
		for _, d := range slices.Sorted(maps.Keys(rel.Deps)) {
			if seen.Add(d) {
				queue = append(queue, item{d, append(slices.Clone(it.path), d)})
			}
		}
	}
	return nil
}

func latest(rels []*Release) *Release {
	var best *Release
	for _, r := range rels {
		if r.Yanked || len(r.Version.Pre) > 0 {
			continue
		}
		if best == nil || cmp.Less(0, r.Version.Compare(best.Version)) {
			best = r
		}
	}
	return best
}
