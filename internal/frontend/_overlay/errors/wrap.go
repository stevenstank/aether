// Aether's replacement of errors/wrap.go (D-049, increment 11.7), which
// uses internal/reflectlite: Unwrap and Is compiled, As declared without a
// body, so that a program reaching it is rejected, until Aether has
// reflection.

package errors

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type has one returning error. Otherwise, Unwrap returns nil.
func Unwrap(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// Is reports whether any error in err's tree matches target: is equal to
// it, if target's type is comparable, or has a method Is(error) bool that
// returns true for it. The tree is searched depth first, through the
// methods Unwrap() error and Unwrap() []error.
func Is(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	return is(err, target, isComparable(target))
}

func is(err, target error, targetComparable bool) bool {
	for {
		if targetComparable && err == target {
			return true
		}
		if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
			if err == nil {
				return false
			}
		case interface{ Unwrap() []error }:
			for _, err := range x.Unwrap() {
				if is(err, target, targetComparable) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}

// isComparable reports whether the dynamic type of err, which is not nil,
// is comparable. It is an intrinsic.
func isComparable(err error) bool

func As(err error, target any) bool
