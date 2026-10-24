// aether:expect unsupported type-switch

package main

func main() {
	var v any = 1
	switch v.(type) {
	}
}
