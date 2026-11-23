package main

import (
	"fmt"
	"time"
)

func main() {
	t, err := time.Parse(time.RFC3339, "2026-03-01T10:15:02Z")
	fmt.Println(t.Truncate(time.Hour).Format("2006-01-02 15:04"), err)
}
