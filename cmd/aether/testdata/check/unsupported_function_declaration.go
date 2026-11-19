// aether:expect unsupported function-declaration

package main

func f[T any](x T) T { return x }

func main() {}
