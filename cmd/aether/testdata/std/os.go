package main

import "os"

func check(n int, err error) {
	if err != nil {
		println("error", err.Error())
		return
	}
	println("wrote", n)
}

func main() {
	println(len(os.Args))
	check(os.Stdout.Write([]byte("hello, stdout\n")))
	check(os.Stdout.WriteString("a string\n"))
	check(os.Stdout.Write(nil))
	check(os.Stderr.WriteString("to stderr\n"))
	println(os.Stdout.Name(), os.Stderr.Name())
	var f *os.File
	check(f.Write([]byte("x")))
	check(f.WriteString("x"))
	check(f.Write(nil))

	// Exit runs no deferred call.
	defer println("not printed")
	os.Stdout.WriteString("exiting\n")
	os.Exit(259)
}
