// Command deps resolves package dependencies against a registry using
// semantic-version constraints.
//
//	deps resolve   -registry r.json -manifest m.json [-trace]
//	deps why PKG   -registry r.json -manifest m.json
//	deps outdated  -registry r.json -manifest m.json
//	deps semver    (reads "constraint | version ..." lines from stdin)
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
)

type registryFile struct {
	Packages map[string]map[string]struct {
		Deps   map[string]string `json:"deps"`
		Yanked bool              `json:"yanked,omitempty"`
	} `json:"packages"`
}

type manifestFile struct {
	Name         string            `json:"name"`
	Dependencies map[string]string `json:"dependencies"`
}

type lockEntry struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Deps    map[string]string `json:"dependencies,omitempty"`
}

type lockFile struct {
	Root     string      `json:"root"`
	Packages []lockEntry `json:"packages"`
	Order    []string    `json:"install_order"`
}

type InputError struct {
	File string
	Err  error
}

func (e *InputError) Error() string { return e.File + ": " + e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &InputError{File: path, Err: err}
	}
	return nil
}

func loadRegistry(path string) (*Registry, error) {
	var rf registryFile
	if err := readJSON(path, &rf); err != nil {
		return nil, err
	}
	reg := &Registry{Packages: map[string][]*Release{}}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(rf.Packages)) {
		versions := rf.Packages[name]
		for _, vs := range slices.Sorted(maps.Keys(versions)) {
			spec := versions[vs]
			v, err := ParseVersion(vs)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			rel := &Release{Name: name, Version: v, Deps: map[string]Constraint{}, Yanked: spec.Yanked}
			for dep, cs := range spec.Deps {
				c, err := ParseConstraint(cs)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s@%s -> %s: %w", name, v, dep, err))
					continue
				}
				rel.Deps[dep] = c
			}
			reg.Packages[name] = append(reg.Packages[name], rel)
		}
	}
	if len(errs) > 0 {
		return nil, &InputError{File: path, Err: errors.Join(errs...)}
	}
	return reg, nil
}

func loadManifest(path string) (string, map[string]Constraint, error) {
	var mf manifestFile
	if err := readJSON(path, &mf); err != nil {
		return "", nil, err
	}
	root := map[string]Constraint{}
	for name, cs := range mf.Dependencies {
		c, err := ParseConstraint(cs)
		if err != nil {
			return "", nil, &InputError{File: path, Err: fmt.Errorf("%s: %w", name, err)}
		}
		root[name] = c
	}
	if mf.Name == "" {
		mf.Name = "root"
	}
	return mf.Name, root, nil
}

func semverMode(in io.Reader, out io.Writer) int {
	sc := bufio.NewScanner(in)
	bad := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndex(line, "|")
		if i < 0 {
			fmt.Fprintf(out, "%s: missing '|'\n", line)
			bad++
			continue
		}
		cs, vs := line[:i], line[i+1:]
		c, err := ParseConstraint(strings.TrimSpace(cs))
		if err != nil {
			fmt.Fprintf(out, "%-22s error: %v\n", strings.TrimSpace(cs), err)
			bad++
			continue
		}
		var parts []string
		var versions []Version
		for _, f := range strings.Fields(vs) {
			v, err := ParseVersion(f)
			if err != nil {
				parts = append(parts, f+"=invalid")
				bad++
				continue
			}
			versions = append(versions, v)
			mark := "no"
			if c.Allows(v) {
				mark = "yes"
			}
			parts = append(parts, v.String()+"="+mark)
		}
		slices.SortStableFunc(versions, Version.Compare)
		var sorted []string
		for _, v := range versions {
			sorted = append(sorted, v.String())
		}
		fmt.Fprintf(out, "%-22s %s  (sorted: %s)\n", c, strings.Join(parts, " "), strings.Join(sorted, " < "))
	}
	if bad > 0 {
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: deps resolve|why|outdated|semver [flags]")
		os.Exit(2)
	}
	cmd := os.Args[1]
	if cmd == "semver" {
		os.Exit(semverMode(os.Stdin, os.Stdout))
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	regPath := fs.String("registry", "", "registry JSON")
	manPath := fs.String("manifest", "", "manifest JSON")
	trace := fs.Bool("trace", false, "trace resolution steps on stderr")
	maxSteps := fs.Int("max-steps", 100000, "give up after this many steps")
	args := os.Args[2:]
	var target string
	if cmd == "why" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	fs.Parse(args)
	if *regPath == "" || *manPath == "" {
		fmt.Fprintln(os.Stderr, "deps: -registry and -manifest are required")
		os.Exit(2)
	}
	reg, err := loadRegistry(*regPath)
	if err == nil {
		var name string
		var root map[string]Constraint
		name, root, err = loadManifest(*manPath)
		if err == nil {
			err = run(cmd, target, name, root, reg, *trace, *maxSteps)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "deps:", err)
		var ie *InputError
		var ce *ConflictError
		switch {
		case errors.As(err, &ie), errors.Is(err, os.ErrNotExist):
			os.Exit(3)
		case errors.As(err, &ce):
			os.Exit(1)
		case errors.Is(err, ErrUnknownPackage):
			os.Exit(4)
		}
		os.Exit(1)
	}
}

func run(cmd, target, name string, root map[string]Constraint, reg *Registry, trace bool, maxSteps int) error {
	r := &Resolver{reg: reg, maxSteps: maxSteps}
	if trace {
		r.trace = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	chosen, err := r.Resolve(root)
	if err != nil {
		return err
	}
	switch cmd {
	case "resolve":
		order, err := InstallOrder(chosen)
		if err != nil {
			return err
		}
		lf := lockFile{Root: name, Order: order}
		for _, n := range slices.Sorted(maps.Keys(chosen)) {
			rel := chosen[n]
			e := lockEntry{Name: n, Version: rel.Version.String()}
			if len(rel.Deps) > 0 {
				e.Deps = map[string]string{}
				for d, c := range rel.Deps {
					e.Deps[d] = c.String()
				}
			}
			lf.Packages = append(lf.Packages, e)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(lf); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "resolved %d packages in %d steps\n", len(chosen), r.steps)
	case "why":
		path := Why(root, chosen, target)
		if path == nil {
			return fmt.Errorf("%s is not in the dependency graph", target)
		}
		for i, p := range path {
			v := ""
			if rel := chosen[p]; rel != nil {
				v = "@" + rel.Version.String()
			}
			fmt.Printf("%s%s%s\n", strings.Repeat("  ", i), p, v)
		}
	case "outdated":
		w := bufio.NewWriter(os.Stdout)
		defer w.Flush()
		fmt.Fprintf(w, "%-14s %-10s %-10s %s\n", "package", "selected", "latest", "held back by")
		for _, n := range slices.Sorted(maps.Keys(chosen)) {
			rel := chosen[n]
			l := latest(reg.Packages[n])
			if l == nil || l.Version.Compare(rel.Version) <= 0 {
				continue
			}
			var blockers []string
			for _, other := range slices.Sorted(maps.Keys(chosen)) {
				if c, ok := chosen[other].Deps[n]; ok && !c.Allows(l.Version) {
					blockers = append(blockers, other+" ("+c.String()+")")
				}
			}
			if c, ok := root[n]; ok && !c.Allows(l.Version) {
				blockers = append(blockers, "root ("+c.String()+")")
			}
			fmt.Fprintf(w, "%-14s %-10s %-10s %s\n", n, rel.Version, l.Version, strings.Join(blockers, ", "))
		}
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
	return nil
}
