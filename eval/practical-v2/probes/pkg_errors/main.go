package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	e1 := errors.New("a")
	j := errors.Join(e1, errors.New("b"))
	var pe *os.PathError
	_, err := os.Open("/nonexistent")
	fmt.Println(errors.Is(j, e1), errors.As(err, &pe), errors.Is(err, os.ErrNotExist), errors.Unwrap(fmt.Errorf("x: %w", e1)) == e1)
}
