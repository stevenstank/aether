// aether:expect unsupported generic-uninstantiated

package main

func f[T any](x T) T { return x }

func main() {}
