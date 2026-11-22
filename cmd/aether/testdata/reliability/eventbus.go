// An event bus: generic topics embed a generic base that counts and
// stores events, subscribers are func values and interface methods
// registered by variadic methods, handlers that panic are recovered and
// reported, and a type switch over each topic's events lists cases that
// some type arguments make redundant or impossible. Workers publish over
// channels; results are sorted before printing.
package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Base[T any] struct {
	name   string
	events []T
}

func (b *Base[T]) Record(vs ...T) int {
	b.events = append(b.events, vs...)
	return len(b.events)
}

func (b Base[T]) Count() int { return len(b.events) }

type Handler[T any] func(topic string, v T) error

type Observer interface {
	Observe(topic string, vs ...any) string
}

type Topic[T comparable] struct {
	Base[T]
	*sync.Mutex
	handlers []Handler[T]
	seen     map[T]int
}

func NewTopic[T comparable](name string) *Topic[T] {
	return &Topic[T]{Base: Base[T]{name: name}, Mutex: &sync.Mutex{}, seen: map[T]int{}}
}

func (t *Topic[T]) Subscribe(hs ...Handler[T]) *Topic[T] {
	t.handlers = append(t.handlers, hs...)
	return t
}

func (t *Topic[T]) Publish(vs ...T) (errs []string) {
	t.Lock()
	defer t.Unlock()
	t.Record(vs...)
	for _, v := range vs {
		t.seen[v]++
		for i, h := range t.handlers {
			if err := safeCall(h, t.name, v); err != nil {
				errs = append(errs, fmt.Sprintf("%s#%d: %v", t.name, i, err))
			}
		}
	}
	return errs
}

func safeCall[T any](h Handler[T], topic string, v T) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h(topic, v)
}

// classify's cases overlap for some T: for T = int the second case never
// matches, and for T = string the third never does.
func classify[T comparable](v any) string {
	switch x := v.(type) {
	case T:
		return fmt.Sprintf("T(%v)", x)
	case int:
		return fmt.Sprintf("int(%d)", x)
	case string, []T:
		return fmt.Sprintf("string-or-slice(%v)", x)
	case nil:
		return "nil"
	}
	return "other"
}

type printer struct{ prefix string }

func (p printer) Observe(topic string, vs ...any) string {
	parts := []string{p.prefix, topic}
	for _, v := range vs {
		parts = append(parts, fmt.Sprint(v))
	}
	return strings.Join(parts, " ")
}

var errOdd = errors.New("odd value")

func main() {
	var obs Observer = printer{"obs"}
	var logMu sync.Mutex
	var log []string
	note := func(vs ...any) {
		logMu.Lock()
		log = append(log, obs.Observe("note", vs...))
		logMu.Unlock()
	}

	ints := NewTopic[int]("ints")
	ints.Subscribe(
		func(topic string, v int) error {
			if v%2 == 1 {
				return errOdd
			}
			return nil
		},
		func(topic string, v int) error {
			if v == 13 {
				var m map[string]int
				m["boom"] = v // a nil map assignment panics
			}
			note(topic, v)
			return nil
		},
	)
	strs := NewTopic[string]("strs").Subscribe(func(topic string, v string) error {
		if v == "" {
			panic("empty string")
		}
		note(topic, len(v), strings.ToUpper(v))
		return nil
	})

	var wg sync.WaitGroup
	results := make(chan []string, 8)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var errs []string
			errs = append(errs, ints.Publish(w*5, w*5+1, w*5+3)...)
			if w == 2 {
				errs = append(errs, ints.Publish(13)...)
				errs = append(errs, strs.Publish("")...)
			}
			errs = append(errs, strs.Publish(fmt.Sprint("w", w), strings.Repeat("x", w))...)
			results <- errs
		}(w)
	}
	wg.Wait()
	close(results)
	var errs []string
	for e := range results {
		errs = append(errs, e...)
	}
	sort.Strings(errs)
	sort.Strings(log)
	fmt.Println("errors:", len(errs))
	for _, e := range errs {
		fmt.Println(" ", e)
	}
	fmt.Println("log:", len(log))
	for _, l := range log {
		fmt.Println(" ", l)
	}
	fmt.Println("counts:", ints.Count(), strs.Count(), ints.Base.Count(), len(ints.seen), len(strs.seen))
	keys := make([]int, 0, len(ints.seen))
	for k := range ints.seen {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	fmt.Println("int keys:", keys)

	for _, v := range []any{1, "s", []int{1}, []string{"a"}, nil, 2.5} {
		fmt.Println(classify[int](v), "|", classify[string](v), "|", classify[float64](v))
	}
}
