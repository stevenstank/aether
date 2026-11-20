//go:build !linux || !amd64

package geom

func Arch() string { return "other" }
