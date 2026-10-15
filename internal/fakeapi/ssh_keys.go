// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
)

type sshKey struct {
	ID    int32
	Alias string
	Key   string
}

func (k *sshKey) json() map[string]any {
	sum := sha256.Sum256([]byte(k.Key))
	return map[string]any{
		"id":          k.ID,
		"alias":       k.Alias,
		"fingerprint": "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
		"key":         k.Key,
	}
}

// SSHKeyIDs returns the IDs of all stored SSH keys.
func (f *Server) SSHKeyIDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.sshKeys)
}

func (f *Server) sshKeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account/ssh-keys/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var items []any
		for _, id := range sortedKeys(f.sshKeys) {
			items = append(items, f.sshKeys[id].json())
		}
		writeJSON(w, http.StatusOK, paginate(r, items))
	})
	mux.HandleFunc("POST /api/account/ssh-keys/{$}", func(w http.ResponseWriter, r *http.Request) {
		b := bodyOf(r)
		key, _ := str(b, "key")
		if !strings.HasPrefix(key, "ssh-") && !strings.HasPrefix(key, "ecdsa-") {
			badRequest(w, "key", "Invalid SSH key.")
			return
		}
		alias, _ := str(b, "alias")
		f.mu.Lock()
		defer f.mu.Unlock()
		// The real API trims whitespace from keys; mimic that so the
		// provider's whitespace-insensitive comparison is exercised.
		k := &sshKey{ID: f.id(), Alias: alias, Key: strings.TrimSpace(key)}
		f.sshKeys[k.ID] = k
		writeJSON(w, http.StatusCreated, k.json())
	})
	mux.HandleFunc("GET /api/account/ssh-keys/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withSSHKey(w, r, func(k *sshKey) { writeJSON(w, http.StatusOK, k.json()) })
	})
	mux.HandleFunc("PATCH /api/account/ssh-keys/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withSSHKey(w, r, func(k *sshKey) {
			if a, ok := str(bodyOf(r), "alias"); ok {
				k.Alias = a
			}
			writeJSON(w, http.StatusOK, k.json())
		})
	})
	mux.HandleFunc("DELETE /api/account/ssh-keys/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withSSHKey(w, r, func(k *sshKey) {
			delete(f.sshKeys, k.ID)
			w.WriteHeader(http.StatusNoContent)
		})
	})
}

func (f *Server) withSSHKey(w http.ResponseWriter, r *http.Request, fn func(*sshKey)) {
	id, ok := pathID(r, "id")
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.sshKeys[id]
	if !ok || k == nil {
		notFound(w)
		return
	}
	fn(k)
}

// SetSSHKeyAlias changes a key's alias out of band, to simulate drift.
func (f *Server) SetSSHKeyAlias(id int32, alias string) {
	f.Do(func() { f.sshKeys[id].Alias = alias })
}

// DeleteSSHKey removes a key out of band.
func (f *Server) DeleteSSHKey(id int32) {
	f.Do(func() { delete(f.sshKeys, id) })
}

func sortedKeys[V any](m map[int32]V) []int32 {
	ids := make([]int32, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
