// A bank simulation: worker goroutines apply transfers under a mutex,
// a ledger goroutine collects results over channels, and select handles
// timeouts-free shutdown with a done channel. Totals are deterministic.
package main

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

type account struct {
	id      int
	balance int64
	history []int64
}

type bank struct {
	mu       sync.Mutex
	accounts map[int]*account
	applied  atomic.Int64
	rejected atomic.Int64
}

var errFunds = errors.New("insufficient funds")
var errSame = errors.New("same account")

func (b *bank) transfer(from, to int, amt int64) error {
	if from == to {
		return errSame
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	f, t := b.accounts[from], b.accounts[to]
	if f.balance < amt {
		return fmt.Errorf("transfer %d->%d of %d: %w", from, to, amt, errFunds)
	}
	f.balance -= amt
	t.balance += amt
	f.history = append(f.history, -amt)
	t.history = append(t.history, amt)
	return nil
}

type req struct {
	from, to int
	amt      int64
}

type outcome struct {
	worker int
	err    error
}

func rng(seed uint64) func() uint64 {
	return func() uint64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return seed
	}
}

func main() {
	const nAcc, nWorkers, nReq = 12, 6, 3000
	b := &bank{accounts: map[int]*account{}}
	var total int64
	for i := range nAcc {
		b.accounts[i] = &account{id: i, balance: int64(100 * (i + 1))}
		total += int64(100 * (i + 1))
	}
	reqs := make(chan req, 64)
	results := make(chan outcome)
	done := make(chan struct{})
	var wg sync.WaitGroup
	for w := range nWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range reqs {
				err := b.transfer(r.from, r.to, r.amt)
				if err == nil {
					b.applied.Add(1)
				} else {
					b.rejected.Add(1)
				}
				results <- outcome{w, err}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	counts := map[string]int{}
	var perWorker [nWorkers]int
	go func() {
		defer func() { close(done) }()
		for o := range results {
			perWorker[o.worker]++
			switch {
			case o.err == nil:
				counts["ok"]++
			case errors.Is(o.err, errFunds):
				counts["funds"]++
			case errors.Is(o.err, errSame):
				counts["same"]++
			default:
				counts["other"]++
			}
		}
	}()
	r := rng(42)
	for range nReq {
		reqs <- req{int(r() % nAcc), int(r() % nAcc), int64(r()%250 + 1)}
	}
	close(reqs)
	select {
	case <-done:
	}
	var sum int64
	ids := make([]int, 0, nAcc)
	for id := range b.accounts {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	moves := 0
	for _, id := range ids {
		a := b.accounts[id]
		sum += a.balance
		moves += len(a.history)
		var net int64
		for _, h := range a.history {
			net += h
		}
		if net != a.balance-int64(100*(id+1)) {
			fmt.Println("history mismatch for", id)
		}
	}
	handled := 0
	for _, n := range perWorker {
		handled += n
	}
	fmt.Println("conserved:", sum == total, "total:", total)
	fmt.Println("handled:", handled, "applied+rejected:", b.applied.Load()+b.rejected.Load())
	fmt.Println("moves even:", moves == 2*counts["ok"], "ok == applied:", int64(counts["ok"]) == b.applied.Load())
	fmt.Println("categories:", counts["ok"]+counts["funds"]+counts["same"], "other:", counts["other"])
}
