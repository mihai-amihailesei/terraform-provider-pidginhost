// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected an error for a missing token")
	}
	if _, err := New(Config{Token: "x", BaseURL: "ftp://example.com"}); err == nil {
		t.Fatal("expected an error for a non-HTTP URL")
	}
	c, err := New(Config{Token: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SDK.GetConfig().Servers[0].URL; got != DefaultBaseURL {
		t.Fatalf("default URL = %q", got)
	}
	if c.PollInterval != DefaultPollInterval {
		t.Fatalf("default poll interval = %s", c.PollInterval)
	}
}

func TestAuthorizationHeaderAndBaseURL(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 7, "alias": "a", "fingerprint": "fp", "key": "ssh-ed25519 AAAA"}`))
	}))
	defer srv.Close()

	c, err := New(Config{Token: "secret", BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := c.Account().AccountSshKeysRetrieve(context.Background(), "7").Execute()
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Token secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotPath != "/api/account/ssh-keys/7/" {
		t.Fatalf("path = %q", gotPath)
	}
	if key.GetId() != 7 {
		t.Fatalf("id = %d", key.GetId())
	}
}

func TestRetryTransport(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		statuses   []int
		wantCalls  int32
		wantStatus int
	}{
		{"429 is retried for POST", http.MethodPost, []int{429, 201}, 2, 201},
		{"503 is retried for GET", http.MethodGet, []int{503, 503, 200}, 3, 200},
		{"503 is not retried for POST", http.MethodPost, []int{503, 201}, 1, 503},
		{"404 is not retried", http.MethodGet, []int{404, 200}, 1, 404},
		{"gives up after max retries", http.MethodGet, []int{502, 502, 502, 502}, 3, 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tt.statuses[n-1])
			}))
			defer srv.Close()

			client := &http.Client{Transport: newRetryTransport(http.DefaultTransport, 2)}
			req, _ := http.NewRequest(tt.method, srv.URL, strings.NewReader(`{"a":1}`))
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if calls.Load() != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
		})
	}
}

func TestErrorDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"key": ["Invalid SSH key."], "alias": ["Too long."]}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Token: "x", BaseURL: srv.URL})
	_, resp, err := c.Account().AccountSshKeysRetrieve(context.Background(), "1").Execute()
	if err == nil {
		t.Fatal("expected an error")
	}
	got := ErrorDetail(err, resp)
	for _, want := range []string{"400", "key: Invalid SSH key.", "alias: Too long.", "/api/account/ssh-keys/1/"} {
		if !strings.Contains(got, want) {
			t.Errorf("ErrorDetail() = %q, missing %q", got, want)
		}
	}
	if IsNotFound(resp) {
		t.Error("IsNotFound() = true for 400")
	}
}

func TestFormatBody(t *testing.T) {
	cases := map[string]string{
		`{"detail": "Not found."}`:  "Not found.",
		`["one", "two"]`:            "one, two",
		`plain text`:                "plain text",
		``:                          "",
		`{"a": {"b": ["c"]}}`:       `a: {"b":["c"]}`,
		`{"non_field_errors": [1]}`: "non_field_errors: 1",
	}
	for in, want := range cases {
		if got := formatBody([]byte(in)); got != want {
			t.Errorf("formatBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIDs(t *testing.T) {
	if id, err := ParseID("42"); err != nil || id != 42 {
		t.Fatalf("ParseID(42) = %d, %v", id, err)
	}
	for _, bad := range []string{"", "0", "-1", "abc", "99999999999"} {
		if _, err := ParseID(bad); err == nil {
			t.Errorf("ParseID(%q) succeeded", bad)
		}
	}
	if FormatID(9) != "9" {
		t.Fatal("FormatID")
	}
	parts, err := SplitID("1/2", 2, "a/b")
	if err != nil || parts[0] != "1" || parts[1] != "2" {
		t.Fatalf("SplitID = %v, %v", parts, err)
	}
	for _, bad := range []string{"1", "1/", "/2", "1/2/3"} {
		if _, err := SplitID(bad, 2, "a/b"); err == nil {
			t.Errorf("SplitID(%q) succeeded", bad)
		}
	}
}

func TestListAll(t *testing.T) {
	pages := [][]int{{1, 2}, {3, 4}, {5}}
	got, err := ListAll(context.Background(), func(_ context.Context, page int32) ([]int, bool, *http.Response, error) {
		return pages[page-1], int(page) < len(pages), nil, nil
	})
	if err != nil || len(got) != 5 || got[4] != 5 {
		t.Fatalf("ListAll = %v, %v", got, err)
	}

	_, err = ListAll(context.Background(), func(_ context.Context, _ int32) ([]int, bool, *http.Response, error) {
		return nil, false, nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestWaitFor(t *testing.T) {
	n := 0
	err := WaitFor(context.Background(), time.Millisecond, "thing", func(context.Context) (bool, string, error) {
		n++
		return n == 3, "pending", nil
	})
	if err != nil || n != 3 {
		t.Fatalf("WaitFor = %v after %d calls", err, n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = WaitFor(ctx, 5*time.Millisecond, "thing", func(context.Context) (bool, string, error) {
		return false, "provisioning", nil
	})
	if err == nil || !strings.Contains(err.Error(), "last state: provisioning") {
		t.Fatalf("WaitFor timeout error = %v", err)
	}
}
