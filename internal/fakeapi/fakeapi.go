// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package fakeapi is an in-memory, stateful stand-in for the parts of the
// Pidgin Host API that the provider uses. Responses are shaped like the
// OpenAPI spec (every property the spec marks required is present, because
// the SDK rejects responses missing one), so tests exercise the real SDK
// decoding as well as the provider logic, without credentials or network.
//
// It models the asynchronous behaviour the spec describes: servers and
// buckets start in a non-active status and become active after a configurable
// number of polls, and deletion of servers and buckets completes after a few
// polls too.
package fakeapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Token is the API token the fake accepts.
const Token = "fake-test-token"

// Request is one request received by the fake.
type Request struct {
	Method string
	Path   string
	Body   map[string]any
}

// Server is the fake API.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	nextID   int32
	requests []Request

	// PollsUntilActive is how many GETs a new server or bucket answers with
	// a non-active status before it becomes active.
	PollsUntilActive int

	// FailProvisioning makes new servers end in status "failed".
	FailProvisioning bool

	images          []map[string]any
	packages        []map[string]any
	storageProducts []map[string]any
	// resource state; see the per-resource files
	state
}

// New starts a fake API and registers its shutdown with t.
func New(t testing.TB) *Server {
	t.Helper()
	f := &Server{
		nextID:           100,
		PollsUntilActive: 2,
	}
	f.initState()
	f.seedCatalog()

	mux := http.NewServeMux()
	f.routes(mux)
	f.Server = httptest.NewServer(f.middleware(mux))
	t.Cleanup(f.Close)
	return f
}

// ProviderConfig returns a provider block pointing at the fake.
func (f *Server) ProviderConfig() string {
	return fmt.Sprintf(`
provider "pidginhost" {
  token   = %q
  api_url = %q
}
`, Token, f.URL)
}

// Requests returns a copy of every request received so far.
func (f *Server) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.requests))
	copy(out, f.requests)
	return out
}

// CountRequests counts requests whose method matches and whose path ends
// with suffix.
func (f *Server) CountRequests(method, suffix string) int {
	n := 0
	for _, r := range f.Requests() {
		if r.Method == method && strings.HasSuffix(r.Path, suffix) {
			n++
		}
	}
	return n
}

// Do runs fn with the fake's lock held, so tests can mutate state to
// simulate out-of-band changes (drift).
func (f *Server) Do(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *Server) id() int32 {
	f.nextID++
	return f.nextID
}

func (f *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token "+Token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "Invalid token."})
			return
		}
		var body map[string]any
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "JSON parse error"})
					return
				}
			}
		}
		f.mu.Lock()
		f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.Path, Body: body})
		f.mu.Unlock()

		ctx := withBody(r.Context(), body)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"detail": "No Server matches the given query."})
}

func badRequest(w http.ResponseWriter, field, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{field: []string{msg}})
}

func pathID(r *http.Request, name string) (int32, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

// paginate wraps items in the DRF page envelope. Pages hold two items so
// that tests exercise the provider's pagination.
func paginate(r *http.Request, items []any) map[string]any {
	const size = 2
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	start := (page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := min(start+size, len(items))
	var next any
	if end < len(items) {
		next = fmt.Sprintf("http://%s%s?page=%d", r.Host, r.URL.Path, page+1)
	}
	var prev any
	if page > 1 {
		prev = fmt.Sprintf("http://%s%s?page=%d", r.Host, r.URL.Path, page-1)
	}
	return map[string]any{"count": len(items), "next": next, "previous": prev, "results": items[start:end]}
}

func str(body map[string]any, key string) (string, bool) {
	v, ok := body[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func boolean(body map[string]any, key string) (bool, bool) {
	v, ok := body[key].(bool)
	return v, ok
}

func integer(body map[string]any, key string) (int, bool) {
	switch v := body[key].(type) {
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	return 0, false
}
