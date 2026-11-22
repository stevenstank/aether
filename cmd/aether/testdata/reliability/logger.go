// A leveled logger in the style of real libraries: Printf-like variadic
// methods, an interface with variadic methods, sinks as variadic func
// values in a map, formatting forwarded with f(g()), deferred variadic
// calls that flush at the end of a scope, and fields gathered by a
// variadic constructor. The output is buffered and printed in order.
package main

import (
	"fmt"
	"strings"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	case Error:
		return "ERROR"
	}
	return fmt.Sprintf("LEVEL(%d)", int(l))
}

type Sink func(level Level, parts ...string)

type Field struct {
	Key string
	Val any
}

func F(kv ...any) []Field {
	var fs []Field
	for i := 0; i+1 < len(kv); i += 2 {
		fs = append(fs, Field{fmt.Sprint(kv[i]), kv[i+1]})
	}
	return fs
}

type Logger interface {
	Logf(level Level, format string, args ...any)
	With(fields ...Field) Logger
}

type logger struct {
	min    Level
	fields []Field
	sinks  map[string]Sink
	order  []string
}

func (l *logger) Logf(level Level, format string, args ...any) {
	if level < l.min {
		return
	}
	msg := fmt.Sprintf(format, args...)
	parts := []string{level.String(), msg}
	for _, f := range l.fields {
		parts = append(parts, fmt.Sprintf("%s=%v", f.Key, f.Val))
	}
	for _, name := range l.order {
		l.sinks[name](level, parts...)
	}
}

func (l *logger) With(fields ...Field) Logger {
	c := *l
	c.fields = append(append([]Field(nil), l.fields...), fields...)
	return &c
}

func (l *logger) AddSink(name string, s Sink) {
	l.sinks[name] = s
	l.order = append(l.order, name)
}

var out strings.Builder

func emit(prefix string) Sink {
	return func(level Level, parts ...string) {
		fmt.Fprintf(&out, "%s %s\n", prefix, strings.Join(parts, " | "))
	}
}

type counter map[Level]int

func (c counter) Sink(level Level, parts ...string) { c[level] += len(parts) }

func request(id int, ok bool) (string, any, any) {
	if ok {
		return "request %d ok in %dms", id, id * 7
	}
	return "request %d failed: %v", id, fmt.Errorf("status %d", 500+id)
}

func handle(log Logger, id int) {
	l := log.With(F("req", id)...)
	defer l.Logf(Debug, "done %d", id)
	defer func(msgs ...string) {
		if r := recover(); r != nil {
			l.Logf(Error, "recovered: %v (%s)", r, strings.Join(msgs, ","))
		}
	}("cleanup", fmt.Sprint(id))
	if id%3 == 0 {
		format, a, b := request(id, false)
		l.Logf(Warn, format, a, b)
		var s []int
		_ = s[id] // out of range: recovered above
	}
	format, a, b := request(id, true)
	l.Logf(Info, format, a, b)
}

func sprintfAll(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func pair() (string, any) { return "pair %v", "!" }

func main() {
	base := &logger{min: Debug, sinks: map[string]Sink{}}
	counts := counter{}
	base.AddSink("main", emit(">"))
	base.AddSink("count", counts.Sink)
	var log Logger = base
	log = log.With(F("svc", "api", "ver", 2, "dangling")...)
	for id := 1; id <= 6; id++ {
		handle(log, id)
	}
	quiet := &logger{min: Warn, sinks: map[string]Sink{"main": emit("q")}, order: []string{"main"}}
	quiet.Logf(Info, "hidden")
	quiet.Logf(Error, "shown %s %d", "x", 1)
	logf := quiet.Logf
	logf(Warn, "via method value %v", []int{1, 2})
	call := (*logger).Logf
	call(quiet, Error, "via method expression")
	fmt.Print(out.String())
	for lv := Debug; lv <= Error+1; lv++ {
		fmt.Println(lv, counts[lv])
	}
	fmt.Println(sprintfAll(pair()), sprintfAll("%d-%d", 1, 2), sprintfAll("none"))
}
