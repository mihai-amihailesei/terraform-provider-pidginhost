// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The SDK alone cannot decode a bucket larger than 2 GiB. If this test
// starts failing because the SDK decodes it fine, the workaround can go.
func TestGetBucketLargeCounters(t *testing.T) {
	const body = `{"id": 5, "name": "logs", "full_name": "acct-logs", "quota_gb": 500,
		"used_bytes": 53687091200, "object_count": 3000000000, "public_read": false,
		"status": "active", "endpoint": "https://s3.example", "region": "ro", "created": "2026-01-01",
		"billing": null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c, _ := New(Config{Token: "x", BaseURL: srv.URL})

	if _, _, err := c.Cloud().CloudBucketsRetrieve(context.Background(), 5).Execute(); err == nil {
		t.Log("the SDK now decodes 64-bit bucket counters; the GetBucket workaround can be removed")
	}

	b, _, err := c.GetBucket(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if b.UsedBytes != 53687091200 || b.ObjectCount != 3000000000 || b.FullName != "acct-logs" {
		t.Fatalf("decoded %+v", b)
	}
}

func TestDecodeAgainKeepsAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail": "Not found."}`))
	}))
	defer srv.Close()
	c, _ := New(Config{Token: "x", BaseURL: srv.URL})
	_, resp, err := c.GetBucket(context.Background(), 5)
	if err == nil || !IsNotFound(resp) {
		t.Fatalf("err = %v, status = %d", err, StatusCode(resp))
	}
}
