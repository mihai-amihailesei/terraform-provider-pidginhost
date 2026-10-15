// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"fmt"
	"net/http"
	"regexp"
)

type bucket struct {
	ID         int32
	Name       string
	QuotaGB    int
	PublicRead bool
	Status     string
	AccessKey  string
	SecretKey  string
	pollsLeft  int
	rotations  int
}

// bigUsage is larger than an int32 can hold, so every bucket response
// exercises the provider's workaround for the SDK's 32-bit counters.
const bigUsage = int64(5) << 30

func (b *bucket) json() map[string]any {
	return map[string]any{
		"id": b.ID, "name": b.Name, "full_name": "acct42-" + b.Name, "quota_gb": b.QuotaGB,
		"used_bytes": bigUsage, "object_count": 1234, "public_read": b.PublicRead, "status": b.Status,
		"endpoint": "https://s3.fake.invalid", "region": "ro-buh-1", "created": "2026-01-02T03:04:05Z",
		"billing": nil,
	}
}

func (b *bucket) credentials() map[string]any {
	return map[string]any{
		"bucket": "acct42-" + b.Name, "endpoint": "https://s3.fake.invalid", "region": "ro-buh-1",
		"access_key": b.AccessKey, "secret_key": b.SecretKey,
	}
}

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// BucketIDs returns the IDs of all buckets.
func (f *Server) BucketIDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.buckets)
}

// SetBucketPublic changes a bucket's visibility out of band.
func (f *Server) SetBucketPublic(id int32, public bool) {
	f.Do(func() { f.buckets[id].PublicRead = public })
}

// RotateBucketKeys rotates a bucket's keys out of band.
func (f *Server) RotateBucketKeys(id int32) {
	f.Do(func() { f.buckets[id].rotate() })
}

func (b *bucket) rotate() {
	b.rotations++
	b.AccessKey = fmt.Sprintf("AK%dR%d", b.ID, b.rotations)
	b.SecretKey = fmt.Sprintf("SK%dR%dsecret", b.ID, b.rotations)
}

func (f *Server) bucketRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cloud/buckets/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []any{}
		for _, id := range sortedKeys(f.buckets) {
			items = append(items, f.buckets[id].json())
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("POST /api/cloud/buckets/{$}", func(w http.ResponseWriter, r *http.Request) {
		b := bodyOf(r)
		name, _ := str(b, "name")
		if !bucketName.MatchString(name) {
			badRequest(w, "name", "Use lowercase letters, digits and hyphens.")
			return
		}
		quota, _ := integer(b, "quota_gb")
		if quota < 1 {
			badRequest(w, "quota_gb", "Ensure this value is greater than or equal to 1.")
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, other := range f.buckets {
			if other.Name == name {
				badRequest(w, "name", "A bucket with this name already exists.")
				return
			}
		}
		bk := &bucket{ID: f.id(), Name: name, QuotaGB: quota, Status: "pending", pollsLeft: f.PollsUntilActive}
		bk.PublicRead, _ = boolean(b, "public_read")
		bk.rotate()
		f.buckets[bk.ID] = bk
		writeJSON(w, http.StatusAccepted, bk.json())
	})
	with := func(w http.ResponseWriter, r *http.Request, fn func(b *bucket)) {
		id, ok := pathID(r, "id")
		f.mu.Lock()
		defer f.mu.Unlock()
		b := f.buckets[id]
		if !ok || b == nil {
			notFound(w)
			return
		}
		fn(b)
	}
	mux.HandleFunc("GET /api/cloud/buckets/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(b *bucket) {
			switch b.Status {
			case "pending", "provisioning":
				if b.pollsLeft--; b.pollsLeft <= 0 {
					b.Status = "active"
				} else {
					b.Status = "provisioning"
				}
			case "cancelling":
				if b.pollsLeft--; b.pollsLeft <= 0 {
					delete(f.buckets, b.ID)
					notFound(w)
					return
				}
			}
			writeJSON(w, http.StatusOK, b.json())
		})
	})
	mux.HandleFunc("DELETE /api/cloud/buckets/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(b *bucket) {
			b.Status, b.pollsLeft = "cancelling", 2
			writeJSON(w, http.StatusAccepted, map[string]any{"id": b.ID, "status": b.Status})
		})
	})
	mux.HandleFunc("POST /api/cloud/buckets/{id}/resize/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(b *bucket) {
			q, _ := integer(bodyOf(r), "quota_gb")
			if q < 1 {
				badRequest(w, "quota_gb", "Ensure this value is greater than or equal to 1.")
				return
			}
			b.QuotaGB = q
			writeJSON(w, http.StatusOK, b.json())
		})
	})
	mux.HandleFunc("POST /api/cloud/buckets/{id}/visibility/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(b *bucket) {
			v, ok := boolean(bodyOf(r), "public_read")
			if !ok {
				badRequest(w, "public_read", "This field is required.")
				return
			}
			b.PublicRead = v
			writeJSON(w, http.StatusOK, b.json())
		})
	})
	mux.HandleFunc("POST /api/cloud/buckets/{id}/credentials/reveal/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(b *bucket) { writeJSON(w, http.StatusOK, b.credentials()) })
	})
	mux.HandleFunc("POST /api/cloud/buckets/{id}/credentials/rotate/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(b *bucket) {
			b.rotate()
			writeJSON(w, http.StatusOK, b.credentials())
		})
	})
}
