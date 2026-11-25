package main

import (
	"fmt"
	"path/filepath"
)

func main() {
	fmt.Println(filepath.Join("a", "b.tmpl"), filepath.Ext("x.tmpl"))
}
