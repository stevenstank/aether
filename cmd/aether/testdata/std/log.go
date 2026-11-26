package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
)

func main() {
	var buf bytes.Buffer
	l := log.New(&buf, "app: ", 0)
	l.Printf("x=%d", 1)
	l.Println("a", 2, true)
	l.Print("no newline")
	l.SetPrefix("[p] ")
	l.SetFlags(log.Lmsgprefix)
	l.Print("msgprefix")
	fmt.Print(buf.String())
	fmt.Println(l.Prefix(), l.Flags(), l.Writer() == &buf)
	std := log.New(os.Stdout, "", 0)
	std.Println("to stdout")
	log.SetOutput(os.Stdout)
	log.SetFlags(0)
	log.Println("default logger")
	defer fmt.Println("not printed: Fatal exits")
	func() {
		defer func() { fmt.Println("recovered:", recover()) }()
		log.Panicf("panic %d", 7)
	}()
	log.Fatalln("fatal", "now")
}
