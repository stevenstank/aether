package main

import (
	"errors"
	"fmt"
)

type E struct{}

func (*E) Error() string { return "e" }

func main() {
	var e *E
	err := fmt.Errorf("wrap: %w", &E{})
	fmt.Println(errors.As(err, &e))
}
