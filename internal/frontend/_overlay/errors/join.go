// Aether's replacement of errors/join.go (D-049): Join is declared without
// a body, so that a program reaching it is rejected, until increment 11.7
// compiles it.

package errors

func Join(errs ...error) error
