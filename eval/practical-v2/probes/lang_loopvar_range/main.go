package main

import "fmt"

func main() {
	var fs []func() string
	for _, op := range []string{"quotient", "remainder"} {
		fs = append(fs, func() string { return op })
	}
	for i := range 2 {
		fs = append(fs, func() string { return fmt.Sprint(i) })
	}
	for _, f := range fs {
		fmt.Println(f())
	}
}
