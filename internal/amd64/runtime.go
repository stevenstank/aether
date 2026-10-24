package amd64

import (
	"embed"
	"io/fs"
	"sort"
)

// The runtime sources are GNU assembly in rt/, a directory without Go
// files: GAS-syntax .s files in a Go package directory would be assembled
// by Go's own assembler and break go build (E9).
//
//go:embed rt/*.s
var runtimeFS embed.FS

// File is a runtime source file.
type File struct {
	Name string // base name, such as "start.s"
	Data []byte
}

// Runtime returns the runtime sources, sorted by name. Every program is
// linked with all of them.
func Runtime() []File {
	names, err := fs.Glob(runtimeFS, "rt/*.s")
	if err != nil {
		panic(err) // the pattern is constant and valid
	}
	sort.Strings(names)
	files := make([]File, len(names))
	for i, name := range names {
		data, err := runtimeFS.ReadFile(name)
		if err != nil {
			panic(err) // embedded files are always readable
		}
		files[i] = File{Name: name[len("rt/"):], Data: data}
	}
	return files
}
