package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	line, err := r.ReadString('\n')
	fmt.Fprintln(w, len(line), err)
}
