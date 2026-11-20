// Aether's replacement of errors/wrap.go (D-049): the functions are
// declared without bodies, so that a program reaching one is rejected,
// until increment 11.7 compiles them.

package errors

func Unwrap(err error) error
func Is(err, target error) bool
func As(err error, target any) bool
