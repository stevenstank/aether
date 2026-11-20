package main

var B = note("B")

var C, D = A * 2, note("D")

func init() { note("more.go init") }

func twice(x int) int { return 2 * x }
