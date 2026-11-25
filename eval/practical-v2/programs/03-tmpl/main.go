// Command tmpl renders a Handlebars-like template language against JSON
// data.
//
// Usage: tmpl [-strict] [-partials dir] -data file.json template-file
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func loadPartials(dir string) (map[string]*Template, error) {
	out := map[string]*Template{}
	if dir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".tmpl" {
			continue
		}
		path := filepath.Join(dir, de.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		t, err := Parse(path, string(src))
		if err != nil {
			return nil, err
		}
		out[strings.TrimSuffix(de.Name(), ".tmpl")] = t
	}
	return out, nil
}

func loadData(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	var v any
	if err := dec.Decode(&v); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			line := bytes.Count(b[:se.Offset], []byte("\n")) + 1
			return nil, fmt.Errorf("%s:%d: invalid JSON: %v", path, line, err)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s: trailing data after JSON value", path)
	}
	return v, nil
}

func main() {
	dataPath := flag.String("data", "", "JSON data file")
	partialDir := flag.String("partials", "", "directory of *.tmpl partials")
	strict := flag.Bool("strict", false, "fail on undefined values")
	listOnly := flag.Bool("list", false, "list partial names and exit")
	flag.Parse()

	partials, err := loadPartials(*partialDir)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			fmt.Fprintf(os.Stderr, "tmpl: cannot read partials: %s: %v\n", pe.Op, pe.Err)
		} else {
			fmt.Fprintln(os.Stderr, "tmpl:", err)
		}
		os.Exit(1)
	}
	if *listOnly {
		names := make([]string, 0, len(partials))
		for n := range partials {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Println(n)
		}
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tmpl [-strict] [-partials dir] [-data file.json] template")
		os.Exit(2)
	}
	var data any = map[string]any{}
	if *dataPath != "" {
		if data, err = loadData(*dataPath); err != nil {
			fmt.Fprintln(os.Stderr, "tmpl:", err)
			os.Exit(1)
		}
	}
	src, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmpl:", err)
		os.Exit(1)
	}
	t, err := Parse(flag.Arg(0), string(src))
	if err == nil {
		var out strings.Builder
		e := &Engine{partials: partials, strict: *strict}
		err = e.Execute(&out, t, data)
		os.Stdout.WriteString(out.String())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmpl:", err)
		var te *TemplateError
		if errors.As(err, &te) && te.Exec {
			if errors.Is(err, ErrUnknownFilter) {
				os.Exit(4)
			}
			os.Exit(3)
		}
		os.Exit(1)
	}
}
