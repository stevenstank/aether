package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

type level int

func (l *level) String() string { return fmt.Sprintf("L%d", int(*l)) }
func (l *level) Set(s string) error {
	switch s {
	case "low":
		*l = 1
	case "high":
		*l = 2
	default:
		return errors.New("want low or high")
	}
	return nil
}

func run(args []string) {
	fs := flag.NewFlagSet("prog", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	n := fs.Int("n", 10, "number of `items`")
	name := fs.String("name", "", "a name")
	verbose := fs.Bool("v", false, "verbose")
	ratio := fs.Float64("ratio", 0.5, "a ratio")
	wait := fs.Duration("wait", 2*time.Second, "how long to wait")
	big := fs.Int64("big", -1, "an int64")
	u := fs.Uint("u", 7, "a uint")
	u64 := fs.Uint64("u64", 0, "a uint64")
	var tags list
	fs.Var(&tags, "tag", "repeatable tag")
	var lv level
	fs.Var(&lv, "level", "low or high")
	var seen []string
	fs.Func("each", "called for each use", func(s string) error {
		if s == "bad" {
			return errors.New("bad value")
		}
		seen = append(seen, s)
		return nil
	})
	fs.BoolFunc("hook", "a bool func", func(s string) error { seen = append(seen, "hook="+s); return nil })
	err := fs.Parse(args)
	fmt.Printf("args=%q err=%v help=%v\n", args, err, errors.Is(err, flag.ErrHelp))
	fmt.Println(*n, *name, *verbose, *ratio, *wait, *big, *u, *u64, tags.String(), lv.String(), seen)
	fmt.Println(fs.NArg(), fs.Args(), fs.Parsed())
	fs.Visit(func(f *flag.Flag) { fmt.Printf("set %s=%s ", f.Name, f.Value) })
	fmt.Println()
	if f := fs.Lookup("n"); f != nil {
		fmt.Println("lookup n:", f.Name, f.DefValue, f.Usage)
	}
	fmt.Println(fs.Set("name", "z"), *name, fs.Set("nosuch", "1"))
}

func main() {
	run([]string{"-n", "3", "--name=bob", "-v", "-ratio", "1.25", "-wait", "1m30s", "x", "-y"})
	run([]string{"-tag", "a", "-tag=b", "-level", "high", "-each", "1", "-each=2", "-hook", "-big", "-9", "--", "-n"})
	run([]string{"-u64", "18446744073709551615", "-v=false", "-u", "4"})
	run([]string{"-n", "x"})
	run([]string{"-level", "mid"})
	run([]string{"-each", "bad"})
	run([]string{"-wait", "soon"})
	run([]string{"-nosuch"})
	run([]string{"-name"})
	run([]string{"-h"})
	run([]string{"-u", "-1"})
	fs := flag.NewFlagSet("visit", flag.ContinueOnError)
	fs.Int("b", 1, "")
	fs.Int("a", 2, "an int")
	fs.String("c", "s", "a `path` here\n\twith two lines")
	fs.SetOutput(os.Stdout)
	fs.VisitAll(func(f *flag.Flag) { fmt.Print(f.Name, " ") })
	fmt.Println()
	fs.PrintDefaults()
	fmt.Println(flag.CommandLine.Name() != "", flag.NArg(), flag.Parsed())
	func() {
		defer func() { fmt.Println("recovered:", recover()) }()
		fs.Int("a", 3, "again")
	}()
}
