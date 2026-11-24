package main

import (
	"errors"
	"fmt"
	"strconv"
)

type NotFound struct{ Name string }

func (e *NotFound) Error() string { return "not found: " + e.Name }

// ValErr has a value receiver, so both ValErr and *ValErr are errors.
type ValErr struct{ Code int }

func (e ValErr) Error() string { return "code " + strconv.Itoa(e.Code) }
func (e ValErr) Temporary() bool { return e.Code >= 500 }

// Matcher converts itself to a *NotFound through an As method.
type Matcher struct{ inner string }

func (m Matcher) Error() string { return "matcher " + m.inner }
func (m Matcher) As(target any) bool {
	if p, ok := target.(**NotFound); ok {
		*p = &NotFound{Name: "via As: " + m.inner}
		return true
	}
	return false
}

// Multi is an error that is not comparable.
type Multi []error

func (m Multi) Error() string { return fmt.Sprintf("%d errors", len(m)) }
func (m Multi) Unwrap() []error { return m }

// Wrapper is named, with the same underlying type as the unnamed
// struct{ *NotFound }, which also implements error by promotion.
type Wrapper struct{ *NotFound }

type temporary interface{ Temporary() bool }

type Coder interface {
	error
	Temporary() bool
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s: panic: %v\n", name, r)
		}
	}()
	f()
}

func main() {
	base := &NotFound{"cfg"}
	wrapped := fmt.Errorf("load: %w", fmt.Errorf("open: %w", base))

	var nf *NotFound
	fmt.Println(errors.As(wrapped, &nf), nf == base, nf)
	var nf2 *NotFound
	fmt.Println(errors.As(errors.New("plain"), &nf2), nf2 == nil)
	fmt.Println(errors.As(nil, &nf2))

	// Value targets.
	var ve ValErr
	fmt.Println(errors.As(fmt.Errorf("x: %w", ValErr{503}), &ve), ve)
	var vp *ValErr
	fmt.Println(errors.As(fmt.Errorf("x: %w", ValErr{1}), &vp), vp == nil)
	fmt.Println(errors.As(fmt.Errorf("x: %w", &ValErr{404}), &vp), *vp)

	// Interface targets: anonymous, named, embedding error, any, error.
	var tmp temporary
	fmt.Println(errors.As(wrapped, &tmp), tmp == nil)
	fmt.Println(errors.As(fmt.Errorf("w: %w", ValErr{502}), &tmp), tmp.Temporary(), tmp)
	var anon interface{ Temporary() bool }
	fmt.Println(errors.As(&ValErr{200}, &anon), anon.Temporary())
	var coder Coder
	fmt.Println(errors.As(fmt.Errorf("w: %w", ValErr{503}), &coder), coder.Error(), coder.Temporary())
	var a any
	fmt.Println(errors.As(wrapped, &a), a == any(wrapped))
	var e error
	fmt.Println(errors.As(wrapped, &e), e == wrapped)

	// The As method, Join, several %w, and nil entries.
	var viaAs *NotFound
	fmt.Println(errors.As(fmt.Errorf("w: %w", Matcher{"m1"}), &viaAs), viaAs)
	joined := errors.Join(errors.New("first"), nil, fmt.Errorf("second: %w", ValErr{7}), &NotFound{"third"})
	var jv ValErr
	var jn *NotFound
	fmt.Println(errors.As(joined, &jv), jv, errors.As(joined, &jn), jn)
	multi := fmt.Errorf("a: %w, b: %w", errors.New("x"), &NotFound{"both"})
	var mn *NotFound
	fmt.Println(errors.As(multi, &mn), mn)
	m := Multi{nil, errors.New("y"), Multi{&NotFound{"deep"}}}
	var dn *NotFound
	fmt.Println(errors.As(m, &dn), dn)
	var mt Multi
	fmt.Println(errors.As(fmt.Errorf("w: %w", m), &mt), len(mt))

	// Is with an uncomparable target, and with comparable ones.
	fmt.Println(errors.Is(m, m), errors.Is(fmt.Errorf("w: %w", base), base), errors.Is(joined, jn))
	fmt.Println(errors.Is(fmt.Errorf("w: %w", ValErr{9}), ValErr{9}), errors.Is(ValErr{9}, ValErr{8}))

	// Assignability between a defined type and an unnamed one.
	var w Wrapper
	var anonErr error = struct{ *NotFound }{&NotFound{"embedded"}}
	fmt.Println(errors.As(fmt.Errorf("w: %w", anonErr), &w), w.Name)
	var back struct{ *NotFound }
	fmt.Println(errors.As(Wrapper{&NotFound{"named"}}, &back), back.Name)

	// Panics.
	try("nil target", func() { errors.As(wrapped, nil) })
	try("non-pointer", func() { errors.As(wrapped, nf) })
	try("nil pointer", func() { errors.As(wrapped, (*error)(nil)) })
	try("not an error", func() { var s string; errors.As(wrapped, &s) })
	try("not an error struct", func() { var x struct{ A int }; errors.As(wrapped, &x) })
	try("value of pointer-method error", func() { var x NotFound; errors.As(wrapped, &x) })
	fmt.Println("done")
}
