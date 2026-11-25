package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Bookmark is the resource the API manages.
type Bookmark struct {
	ID      int       `json:"id"`
	URL     string    `json:"url"`
	Title   string    `json:"title"`
	Tags    []string  `json:"tags"`
	Owner   string    `json:"owner"`
	Created time.Time `json:"created"`
	Version int       `json:"version"`
}

type Store struct {
	mu     sync.RWMutex
	items  map[int]*Bookmark
	nextID int
	clock  func() time.Time
}

func NewStore(clock func() time.Time) *Store {
	return &Store{items: map[int]*Bookmark{}, nextID: 1, clock: clock}
}

type apiError struct {
	Status  int               `json:"-"`
	Code    string            `json:"error"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

var errNotFound = &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "no such bookmark"}

type bookmarkInput struct {
	URL   *string  `json:"url"`
	Title *string  `json:"title"`
	Tags  []string `json:"tags"`
}

func validate(in bookmarkInput, partial bool) *apiError {
	fields := map[string]string{}
	if in.URL == nil {
		if !partial {
			fields["url"] = "required"
		}
	} else if u := *in.URL; !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) || len(u) > 2048 {
		fields["url"] = "must be an http(s) URL of at most 2048 bytes"
	}
	if in.Title != nil {
		if n := utf8.RuneCountInString(strings.TrimSpace(*in.Title)); n == 0 || n > 200 {
			fields["title"] = "must be 1-200 characters"
		}
	} else if !partial {
		fields["title"] = "required"
	}
	if len(in.Tags) > 10 {
		fields["tags"] = "at most 10 tags"
	}
	for _, t := range in.Tags {
		if t == "" || strings.ContainsAny(t, " ,") {
			fields["tags"] = fmt.Sprintf("invalid tag %q", t)
		}
	}
	if len(fields) > 0 {
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "invalid", Message: "validation failed", Fields: fields}
	}
	return nil
}

func normTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		t = strings.ToLower(t)
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Store) Create(owner string, in bookmarkInput) (*Bookmark, *apiError) {
	if err := validate(in, false); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.items {
		if b.Owner == owner && b.URL == *in.URL {
			return nil, &apiError{Status: http.StatusConflict, Code: "duplicate", Message: fmt.Sprintf("bookmark %d already has this URL", b.ID)}
		}
	}
	b := &Bookmark{ID: s.nextID, URL: *in.URL, Title: strings.TrimSpace(*in.Title), Tags: normTags(in.Tags),
		Owner: owner, Created: s.clock(), Version: 1}
	s.nextID++
	s.items[b.ID] = b
	return b, nil
}

func (s *Store) Get(owner string, id int) (Bookmark, *apiError) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.items[id]
	if !ok || b.Owner != owner {
		return Bookmark{}, errNotFound
	}
	return *b, nil
}

func (s *Store) Update(owner string, id int, in bookmarkInput, ifMatch int) (Bookmark, *apiError) {
	if err := validate(in, true); err != nil {
		return Bookmark{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.items[id]
	if !ok || b.Owner != owner {
		return Bookmark{}, errNotFound
	}
	if ifMatch > 0 && ifMatch != b.Version {
		return Bookmark{}, &apiError{Status: http.StatusPreconditionFailed, Code: "version_mismatch",
			Message: fmt.Sprintf("current version is %d", b.Version)}
	}
	if in.URL != nil {
		b.URL = *in.URL
	}
	if in.Title != nil {
		b.Title = strings.TrimSpace(*in.Title)
	}
	if in.Tags != nil {
		b.Tags = normTags(in.Tags)
	}
	b.Version++
	return *b, nil
}

func (s *Store) Delete(owner string, id int) *apiError {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.items[id]
	if !ok || b.Owner != owner {
		return errNotFound
	}
	delete(s.items, id)
	return nil
}

func (s *Store) List(owner, tag, q string) []Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Bookmark
	for _, b := range s.items {
		if b.Owner != owner {
			continue
		}
		if tag != "" && !slices.Contains(b.Tags, strings.ToLower(tag)) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(b.Title+" "+b.URL), strings.ToLower(q)) {
			continue
		}
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b Bookmark) int { return a.ID - b.ID })
	return out
}

func (s *Store) TagCounts(owner string) map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[string]int{}
	for _, b := range s.items {
		if b.Owner == owner {
			for _, t := range b.Tags {
				counts[t]++
			}
		}
	}
	return counts
}

// Server wires the store to HTTP.
type Server struct {
	store    *Store
	tokens   map[string]string // bearer token -> user
	mux      *http.ServeMux
	requests atomic.Int64
	logger   *log.Logger
}

type ctxKey struct{}

func NewServer(store *Store, tokens map[string]string, logOut io.Writer) *Server {
	s := &Server{store: store, tokens: tokens, mux: http.NewServeMux(), logger: log.New(logOut, "", 0)}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/bookmarks", s.auth(s.list))
	s.mux.HandleFunc("POST /api/bookmarks", s.auth(s.create))
	s.mux.HandleFunc("GET /api/bookmarks/{id}", s.auth(s.get))
	s.mux.HandleFunc("PATCH /api/bookmarks/{id}", s.auth(s.update))
	s.mux.HandleFunc("DELETE /api/bookmarks/{id}", s.auth(s.remove))
	s.mux.HandleFunc("GET /api/tags", s.auth(s.tags))
	s.mux.HandleFunc("GET /debug/panic", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]int
		m["boom"]++ // deliberate nil-map write: exercises the recovery middleware
	})
	return s
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// ServeHTTP adds request IDs, access logging, and panic recovery.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := s.requests.Add(1)
	rec := &statusRecorder{ResponseWriter: w}
	rec.Header().Set("X-Request-Id", strconv.FormatInt(id, 10))
	defer func() {
		if p := recover(); p != nil {
			s.logger.Printf("req=%d panic: %v", id, p)
			writeJSON(rec, http.StatusInternalServerError, &apiError{Code: "internal", Message: "internal server error"})
		}
		s.logger.Printf("req=%d %s %s -> %d (%d bytes)", id, r.Method, r.URL.RequestURI(), rec.status, rec.bytes)
	}()
	s.mux.ServeHTTP(rec, r)
}

type handler func(w http.ResponseWriter, r *http.Request, user string)

func (s *Server) auth(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, known := s.tokens[tok]
		if !ok || !known {
			w.Header().Set("WWW-Authenticate", `Bearer realm="bookmarks"`)
			writeJSON(w, http.StatusUnauthorized, &apiError{Code: "unauthorized", Message: "missing or invalid token"})
			return
		}
		h(w, r, user)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
	w.WriteHeader(status)
	w.Write(append(body, '\n'))
}

func writeErr(w http.ResponseWriter, e *apiError) { writeJSON(w, e.Status, e) }

func pathID(r *http.Request) (int, *apiError) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return 0, &apiError{Status: http.StatusBadRequest, Code: "bad_id", Message: fmt.Sprintf("invalid id %q", r.PathValue("id"))}
	}
	return id, nil
}

func decodeBody(r *http.Request, v any) *apiError {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return &apiError{Status: http.StatusUnsupportedMediaType, Code: "bad_content_type", Message: "expected application/json"}
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		msg := err.Error()
		switch {
		case errors.As(err, &se):
			msg = fmt.Sprintf("malformed JSON at offset %d", se.Offset)
		case errors.As(err, &te):
			msg = fmt.Sprintf("field %q must be %s", te.Field, te.Type)
		case errors.Is(err, io.EOF):
			msg = "empty body"
		}
		return &apiError{Status: http.StatusBadRequest, Code: "bad_json", Message: msg}
	}
	return nil
}

func etag(b Bookmark) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d/%d/%s/%s/%v", b.ID, b.Version, b.URL, b.Title, b.Tags)
	return fmt.Sprintf(`"%016x"`, h.Sum64())
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, user string) {
	q := r.URL.Query()
	limit, offset := 20, 0
	params := []struct {
		name string
		dst  *int
	}{{"limit", &limit}, {"offset", &offset}}
	for _, p := range params {
		name, dst := p.name, p.dst
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || (name == "limit" && (n == 0 || n > 100)) {
				writeErr(w, &apiError{Status: http.StatusBadRequest, Code: "bad_query", Message: "invalid " + name})
				return
			}
			*dst = n
		}
	}
	all := s.store.List(user, q.Get("tag"), q.Get("q"))
	page := all[min(offset, len(all)):]
	page = page[:min(limit, len(page))]
	if page == nil {
		page = []Bookmark{}
	}
	if offset+limit < len(all) {
		w.Header().Set("Link", fmt.Sprintf(`</api/bookmarks?offset=%d&limit=%d>; rel="next"`, offset+limit, limit))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, user string) {
	var in bookmarkInput
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.store.Create(user, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/bookmarks/%d", b.ID))
	w.Header().Set("ETag", etag(*b))
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request, user string) {
	id, aerr := pathID(r)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	b, aerr := s.store.Get(user, id)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	tag := etag(b)
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request, user string) {
	id, aerr := pathID(r)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	var in bookmarkInput
	if aerr := decodeBody(r, &in); aerr != nil {
		writeErr(w, aerr)
		return
	}
	ifMatch := 0
	if v := r.Header.Get("If-Match-Version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, &apiError{Status: http.StatusBadRequest, Code: "bad_header", Message: "If-Match-Version must be a number"})
			return
		}
		ifMatch = n
	}
	b, aerr := s.store.Update(user, id, in, ifMatch)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	w.Header().Set("ETag", etag(b))
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request, user string) {
	id, aerr := pathID(r)
	if aerr != nil {
		writeErr(w, aerr)
		return
	}
	if aerr := s.store.Delete(user, id); aerr != nil {
		writeErr(w, aerr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request, user string) {
	counts := s.store.TagCounts(user)
	type tc struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := []tc{}
	for t, n := range counts {
		out = append(out, tc{t, n})
	}
	slices.SortFunc(out, func(a, b tc) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	writeJSON(w, http.StatusOK, out)
}
