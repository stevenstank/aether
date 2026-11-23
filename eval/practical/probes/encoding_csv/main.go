package main

import (
	"encoding/csv"
	"fmt"
	"strings"
)

func main() {
	recs, err := csv.NewReader(strings.NewReader("a,b\n1,2\n")).ReadAll()
	fmt.Println(recs, err)
}
