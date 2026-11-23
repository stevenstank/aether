// Command kvstore runs a script of key-value commands against an in-memory
// store with transactions and a write-ahead log.
//
// Each line of standard input is "[client:] COMMAND args...". Commands:
// PING, GET, SET, DEL, INCR, SCAN, BEGIN, COMMIT, ROLLBACK, DBSIZE, and the
// driver commands:
//
//	LOAD CLIENTS OPS   run CLIENTS concurrent clients doing OPS increments each
//	CHECKPOINT         replay the log into a new store and compare digests
//	CORRUPT OFFSET     flip a bit in the log
//	TEAR N             drop the last N bytes of the log
//	STATS              log size and batch count
//
// Exit status: 0, or 1 if recovery found a mismatch or corruption.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"example.com/kvstore/proto"
	"example.com/kvstore/store"
	"example.com/kvstore/wal"
)

func (s *server) call(cmd proto.Command) proto.Reply {
	ch := make(chan proto.Reply, 1)
	s.reqs <- request{cmd, ch}
	return <-ch
}

// load runs clients concurrently; each increments its own counter and a
// shared one, retrying while another client holds a transaction.
func load(s *server, clients, ops int) string {
	var wg sync.WaitGroup
	var mu sync.Mutex
	locked := 0
	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := "load" + strconv.Itoa(id)
			for i := 0; i < ops; i++ {
				s.call(proto.Command{Client: name, Name: "INCR", Args: []string{"counter:" + name}})
				for {
					r := s.call(proto.Command{Client: name, Name: "INCR", Args: []string{"counter:total"}})
					if r.Kind != proto.Err {
						break
					}
					mu.Lock()
					locked++
					mu.Unlock()
				}
			}
		}(c)
	}
	wg.Wait()
	total := s.call(proto.Command{Client: "main", Name: "GET", Args: []string{"counter:total"}})
	return fmt.Sprintf("load: %d clients x %d ops, counter:total = %s", clients, ops, total.Str)
}

func checkpoint(s *server) (string, error) {
	fresh := store.New()
	n, err := wal.Replay(s.log.Bytes(), fresh.Apply)
	if err != nil {
		switch {
		case errors.Is(err, wal.ErrCorrupt):
			return "", fmt.Errorf("recovery stopped after %d batches: corruption: %v", n, err)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return "", fmt.Errorf("recovery stopped after %d batches: torn write: %v", n, err)
		}
		return "", err
	}
	// Uncommitted transactions are not in the log: compare against the
	// store's committed state only when no transaction is open.
	if s.st.Depth() > 0 {
		return fmt.Sprintf("checkpoint: replayed %d batches, %d keys (transaction open, not compared)", n, fresh.Len()), nil
	}
	if fresh.Digest() != s.st.Digest() || fresh.Len() != s.st.Len() {
		return "", fmt.Errorf("recovered store differs: %d keys %016x, live %d keys %016x",
			fresh.Len(), fresh.Digest(), s.st.Len(), s.st.Digest())
	}
	return fmt.Sprintf("checkpoint: replayed %d batches, %d keys, digest %016x ok", n, fresh.Len(), fresh.Digest()), nil
}

func main() {
	s := newServer()
	go s.serve()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	failed := false
	in := bufio.NewScanner(os.Stdin)
	for lineNo := 1; in.Scan(); lineNo++ {
		line := strings.TrimSpace(in.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cmd, err := proto.Parse(line)
		if err != nil {
			fmt.Fprintf(out, "%d> (error) %v\n", lineNo, err)
			continue
		}
		prefix := fmt.Sprintf("%d> ", lineNo)
		if cmd.Client != "main" {
			prefix = fmt.Sprintf("%d %s> ", lineNo, cmd.Client)
		}
		switch cmd.Name {
		case "LOAD":
			if len(cmd.Args) != 2 {
				fmt.Fprintln(out, prefix+"(error) LOAD takes CLIENTS OPS")
				continue
			}
			c, err1 := strconv.Atoi(cmd.Args[0])
			n, err2 := strconv.Atoi(cmd.Args[1])
			if err1 != nil || err2 != nil || c <= 0 || n <= 0 {
				fmt.Fprintln(out, prefix+"(error) bad LOAD arguments")
				continue
			}
			fmt.Fprintln(out, prefix+load(s, c, n))
		case "CHECKPOINT":
			msg, err := checkpoint(s)
			if err != nil {
				failed = true
				fmt.Fprintln(out, prefix+"RECOVERY FAILED: "+err.Error())
				continue
			}
			fmt.Fprintln(out, prefix+msg)
		case "CORRUPT", "TEAR":
			if len(cmd.Args) != 1 {
				fmt.Fprintln(out, prefix+"(error) need one argument")
				continue
			}
			v, err := strconv.Atoi(cmd.Args[0])
			if err != nil {
				fmt.Fprintln(out, prefix+"(error) not a number")
				continue
			}
			if cmd.Name == "TEAR" {
				s.log.Truncate(v)
			} else if err := s.log.Corrupt(v); err != nil {
				fmt.Fprintln(out, prefix+"(error) "+err.Error())
				continue
			}
			fmt.Fprintln(out, prefix+"OK")
		case "STATS":
			fmt.Fprintf(out, "%slog: %d bytes, %d batches\n", prefix, s.log.Size(), s.log.Batches)
		default:
			r := s.call(cmd)
			text := proto.Format(r)
			fmt.Fprintln(out, prefix+strings.ReplaceAll(text, "\n", "\n"+strings.Repeat(" ", len(prefix))))
		}
	}
	close(s.reqs)
	if failed {
		out.Flush()
		os.Exit(1)
	}
}
