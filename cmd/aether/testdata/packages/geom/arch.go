//go:build linux && amd64 && go1.26 && !go1.27

package geom

func Arch() string { return "linux/amd64" }
