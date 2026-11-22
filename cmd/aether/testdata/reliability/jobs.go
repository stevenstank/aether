// A job scheduler under allocation pressure: jobs are variadic func
// values with their arguments, sent over channels to workers; each
// worker defers variadic calls whose arguments are the only references
// to freshly allocated data, and some jobs panic, so the deferred calls
// run while a panic unwinds. Job keys are structs switched on and used
// in maps. Many short-lived slices keep the collector busy.
package main

import (
	"fmt"
	"strings"
)

type Key struct {
	Queue string
	ID    int
	Tags  [3]string
}

type Job struct {
	Key
	Run  func(args ...*[]int) int
	Args []*[]int
}

type Report struct {
	Key
	Result int
	Notes  []string
}

func fill(n, seed int) *[]int {
	s := make([]int, n)
	for i := range s {
		s[i] = (seed*31 + i*7) % 101
	}
	return &s
}

func total(args ...*[]int) int {
	t := 0
	for _, a := range args {
		for _, x := range *a {
			t += x
		}
	}
	return t
}

func longest(args ...*[]int) int {
	m := 0
	for _, a := range args {
		m = max(m, len(*a))
	}
	return m
}

func checksum(parts ...[]string) string {
	var b strings.Builder
	for _, p := range parts {
		for _, s := range p {
			b.WriteString(s)
			b.WriteByte(';')
		}
	}
	return fmt.Sprintf("%d:%x", b.Len(), len(parts))
}

func priority(k Key) int {
	switch k.Tags {
	case [3]string{"urgent"}:
		return 0
	case [3]string{"urgent", "retry"}:
		return 1
	}
	switch k {
	case Key{Queue: "batch", ID: 7}:
		return 9
	}
	return 5
}

func worker(id int, jobs <-chan Job, out chan<- Report) {
	for j := range jobs {
		out <- runJob(id, j)
	}
}

func runJob(w int, j Job) (r Report) {
	r.Key = j.Key
	note := func(parts ...string) {
		r.Notes = append(r.Notes, strings.Join(parts, " "))
	}
	// The deferred calls' arguments are evaluated now; the slices they
	// hold are reachable only from the defer records.
	defer note("checksum", checksum([]string{j.Queue}, strings.Fields("a b c"), make([]string, j.ID%4)))
	defer func(extra ...*[]int) {
		if p := recover(); p != nil {
			note("recovered", fmt.Sprint(p), fmt.Sprint(total(extra...)))
			r.Result = -1
		}
	}(fill(50, j.ID), fill(j.ID%5, w*0))
	garbage := 0
	for i := 0; i < 40; i++ {
		g := make([]*[]int, 0, 4)
		for k := 0; k < 4; k++ {
			g = append(g, fill(16+k, i))
		}
		garbage += total(g...) % 3
	}
	if j.ID%6 == 5 {
		var arr []int
		_ = arr[j.ID]
	}
	r.Result = j.Run(j.Args...) + garbage*0
	note("priority", fmt.Sprint(priority(j.Key)))
	return r
}

func main() {
	jobs := make(chan Job)
	out := make(chan Report)
	for w := 0; w < 4; w++ {
		go worker(w, jobs, out)
	}
	var keys []Key
	go func() {
		for id := 0; id < 24; id++ {
			k := Key{Queue: []string{"batch", "web", "mail"}[id%3], ID: id}
			switch id % 4 {
			case 0:
				k.Tags = [3]string{"urgent"}
			case 1:
				k.Tags = [3]string{"urgent", "retry"}
			}
			run := total
			if id%2 == 1 {
				run = longest
			}
			keys = append(keys, k)
			jobs <- Job{k, run, []*[]int{fill(id+1, id), fill(3, id+1), fill(id%7, 2)}}
		}
		close(jobs)
	}()
	byKey := map[Key]Report{}
	for i := 0; i < 24; i++ {
		r := <-out
		byKey[r.Key] = r
	}
	sum := 0
	for _, k := range keys {
		r := byKey[k]
		sum += r.Result
		fmt.Printf("%-5s %2d %-14q %5d %s\n", k.Queue, k.ID, strings.Join(k.Tags[:], ","), r.Result, strings.Join(r.Notes, "; "))
	}
	fmt.Println("jobs", len(byKey), "sum", sum)
}
