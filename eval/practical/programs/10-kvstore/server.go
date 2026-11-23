package main

import (
	"errors"
	"fmt"
	"strconv"

	"example.com/kvstore/proto"
	"example.com/kvstore/store"
	"example.com/kvstore/wal"
)

type request struct {
	cmd   proto.Command
	reply chan proto.Reply
}

// session is the per-client state the server keeps. Transactions are
// per-client, so while one client has a transaction open, other clients'
// writes are refused (a single global lock, as in a simple embedded DB).
type session struct{ depth int }

type server struct {
	st       *store.Store
	log      *wal.Log
	sessions map[string]*session
	owner    string // client holding the open transaction, if any
	reqs     chan request
}

func newServer() *server {
	s := &server{
		st:       store.New(),
		log:      &wal.Log{},
		sessions: map[string]*session{},
		reqs:     make(chan request),
	}
	s.st.OnCommit = s.log.Append
	return s
}

func (s *server) serve() {
	for req := range s.reqs {
		req.reply <- s.handle(req.cmd)
	}
}

func errReply(err error) proto.Reply { return proto.Reply{Kind: proto.Err, Str: err.Error()} }

func errorf(format string, args ...any) proto.Reply {
	return proto.Reply{Kind: proto.Err, Str: fmt.Sprintf(format, args...)}
}

var writes = map[string]bool{"SET": true, "DEL": true, "INCR": true, "BEGIN": true, "COMMIT": true, "ROLLBACK": true}

func (s *server) handle(c proto.Command) proto.Reply {
	sess := s.sessions[c.Client]
	if sess == nil {
		sess = &session{}
		s.sessions[c.Client] = sess
	}
	if writes[c.Name] && s.owner != "" && s.owner != c.Client {
		return errorf("LOCKED by %s", s.owner)
	}
	nargs := func(n int) bool { return len(c.Args) == n }
	switch c.Name {
	case "PING":
		return proto.Reply{Kind: proto.Str, Str: "PONG"}
	case "GET":
		if !nargs(1) {
			return errorf("GET takes 1 argument")
		}
		v, err := s.st.Get(c.Args[0])
		if errors.Is(err, store.ErrNotFound) {
			return proto.Reply{Kind: proto.Nil}
		}
		return proto.Reply{Kind: proto.Str, Str: v}
	case "SET":
		if !nargs(2) {
			return errorf("SET takes 2 arguments")
		}
		s.st.Set(c.Args[0], c.Args[1])
		return proto.Reply{Kind: proto.OK}
	case "DEL":
		if len(c.Args) == 0 {
			return errorf("DEL takes at least 1 argument")
		}
		n := 0
		for _, k := range c.Args {
			if s.st.Delete(k) == nil {
				n++
			}
		}
		return proto.Reply{Kind: proto.Int, Int: int64(n)}
	case "INCR":
		delta := int64(1)
		switch len(c.Args) {
		case 1:
		case 2:
			d, err := strconv.ParseInt(c.Args[1], 10, 64)
			if err != nil {
				return errorf("INCR delta %q is not an integer", c.Args[1])
			}
			delta = d
		default:
			return errorf("INCR takes 1 or 2 arguments")
		}
		n, err := s.st.Incr(c.Args[0], delta)
		if err != nil {
			return errReply(err)
		}
		return proto.Reply{Kind: proto.Int, Int: n}
	case "SCAN":
		if len(c.Args) < 1 || len(c.Args) > 3 {
			return errorf("SCAN takes PREFIX [START [LIMIT]]")
		}
		start, limit := "", 10
		if len(c.Args) >= 2 {
			start = c.Args[1]
		}
		if len(c.Args) == 3 {
			l, err := strconv.Atoi(c.Args[2])
			if err != nil || l <= 0 {
				return errorf("bad SCAN limit %q", c.Args[2])
			}
			limit = l
		}
		return proto.Reply{Kind: proto.List, Items: s.st.Scan(c.Args[0], start, limit)}
	case "BEGIN":
		s.st.Begin()
		sess.depth++
		s.owner = c.Client
		return proto.Reply{Kind: proto.Int, Int: int64(sess.depth)}
	case "COMMIT", "ROLLBACK":
		var err error
		if c.Name == "COMMIT" {
			err = s.st.Commit()
		} else {
			err = s.st.Rollback()
		}
		if err != nil {
			return errReply(err)
		}
		sess.depth--
		if sess.depth == 0 {
			s.owner = ""
		}
		return proto.Reply{Kind: proto.OK}
	case "DBSIZE":
		return proto.Reply{Kind: proto.Int, Int: int64(s.st.Len())}
	}
	return errorf("unknown command %q", c.Name)
}
