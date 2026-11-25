package main

import (
	"fmt"
	"html"
)

func main() {
	fmt.Println(html.EscapeString("<a&b>"), html.UnescapeString("&amp;&lt;"))
}
