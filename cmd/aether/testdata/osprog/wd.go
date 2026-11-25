// The working directory: the test runs it in a fresh directory with
// ROOT and PWD set to its path.
package main

import (
	"fmt"
	"os"
	"strings"
)

func wd() {
	d, err := os.Getwd()
	fmt.Printf("%q %v\n", strings.TrimPrefix(d, os.Getenv("ROOT")), err)
}

func main() {
	wd()
	check := func(what string, err error) { fmt.Println(what, err) }
	check("mkdir", os.MkdirAll("sub/deeper", 0o755))
	check("chdir", os.Chdir("sub"))
	wd()
	check("write", os.WriteFile("here.txt", []byte("here"), 0o644))
	check("chdir deeper", os.Chdir("deeper"))
	b, err := os.ReadFile("../here.txt")
	fmt.Printf("%q %v\n", b, err)
	check("chdir missing", os.Chdir("missing"))
	check("chdir file", os.Chdir("../here.txt"))
	check("chdir NUL", os.Chdir("a\x00"))
	wd()
	check("chdir root", os.Chdir(os.Getenv("ROOT")))
	wd()
	os.Setenv("PWD", "/")
	wd()
	os.Setenv("PWD", "relative")
	wd()
	check("chdir /", os.Chdir("/"))
	wd()
}
