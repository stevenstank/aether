// aether:expect unsupported import
// aether:gc skip building needs a module that provides example.com/lib; outside one, gc cannot resolve the import

package main

import "example.com/lib"

func main() {
	lib.F()
}
