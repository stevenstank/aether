package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	l := log.New(os.Stdout, "", 0)
	l.Printf("x=%d", 1)
	fmt.Println("ok")
}
