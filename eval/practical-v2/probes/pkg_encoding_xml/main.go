package main

import (
	"encoding/xml"
	"fmt"
)

func main() {
	type U struct {
		XMLName xml.Name `xml:"urlset"`
		Loc     []string `xml:"url>loc"`
	}
	b, err := xml.MarshalIndent(U{Loc: []string{"a"}}, "", " ")
	fmt.Println(string(b), err)
}
