// Aether's replacement of errors/join.go (D-049, increment 11.7), which
// uses unsafe.

package errors

// Join returns an error that wraps the given errors, leaving out nil ones,
// or nil if they are all nil. Its Error is theirs, separated by newlines.
func Join(errs ...error) error {
	var e *joinError
	for _, err := range errs {
		if err == nil {
			continue
		}
		if e == nil {
			e = &joinError{}
		}
		e.errs = append(e.errs, err)
	}
	if e == nil {
		return nil
	}
	return e
}

type joinError struct {
	errs []error
}

func (e *joinError) Error() string {
	s := e.errs[0].Error()
	for _, err := range e.errs[1:] {
		s += "\n" + err.Error()
	}
	return s
}

func (e *joinError) Unwrap() []error {
	return e.errs
}
