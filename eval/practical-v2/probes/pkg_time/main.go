package main

import (
	"fmt"
	"time"
)

func main() {
	t, err := time.Parse(time.RFC3339Nano, "2026-03-14T10:22:31.5+02:00")
	d, _ := time.ParseDuration("1h2m450us")
	fmt.Println(t.UTC().Format(time.RFC3339), err, d, t.Truncate(time.Hour).Unix(), t.Format("Monday, 2 January 2006"))
	tm := time.NewTimer(time.Millisecond)
	<-tm.C
	fmt.Println("timer")
}
