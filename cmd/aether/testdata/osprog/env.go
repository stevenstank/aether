// The environment: the test runs it with exactly A=1, B=, PATHS=x:y, and
// HOME and TMPDIR unset or set by the case.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Environ())
	for _, k := range []string{"A", "B", "C", "", "PATHS"} {
		v, ok := os.LookupEnv(k)
		fmt.Printf("%q: %q %v %q\n", k, v, ok, os.Getenv(k))
	}
	fmt.Println(os.ExpandEnv("a=$A b=${B}! c=$C paths=${PATHS} $$ $1 ${} ${ $"))
	fmt.Println(os.Expand("$x/${yy}/$", func(s string) string { return "<" + s + ">" }))
	for _, kv := range [][2]string{{"C", "3"}, {"A", "one"}, {"", "x"}, {"K=V", "x"}, {"K\x00", "x"}, {"K", "v\x00"}} {
		err := os.Setenv(kv[0], kv[1])
		fmt.Printf("setenv %q: %v", kv[0], err)
		if err != nil {
			se, ok := err.(*os.SyscallError)
			fmt.Printf(" %v %s", ok, se.Syscall)
		}
		fmt.Println()
	}
	fmt.Println(os.Environ())
	fmt.Println(os.Unsetenv("A"), os.Unsetenv("nothing"))
	fmt.Println(os.Environ(), os.Getenv("A"))
	fmt.Println(os.TempDir())
	home, err := os.UserHomeDir()
	fmt.Printf("%q %v\n", home, err)
	os.Clearenv()
	fmt.Println(os.Environ(), len(os.Environ()), os.Getenv("C"))
	os.Setenv("D", "4")
	fmt.Println(os.Environ(), os.TempDir())
}
