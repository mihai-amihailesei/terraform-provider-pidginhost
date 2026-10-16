// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"net/http"
	"slices"
)

func (f *Server) seedCatalog() {
	f.images = []map[string]any{
		{"id": 1, "slug": "ubuntu-24-04", "name": "Ubuntu 24.04", "family_name": "Ubuntu", "is_default": true, "default_username": "ubuntu"},
		{"id": 2, "slug": "ubuntu-22-04", "name": "Ubuntu 22.04", "family_name": "Ubuntu", "is_default": false, "default_username": "ubuntu"},
		{"id": 3, "slug": "debian-12", "name": "Debian 12", "family_name": "Debian", "is_default": true, "default_username": "debian"},
		{"id": 4, "slug": "custom", "name": "Custom", "family_name": nil, "is_default": false, "default_username": "root"},
	}
	f.packages = []map[string]any{
		{"id": 11, "slug": "cloud-xs", "name": "Cloud XS", "cpus": 1, "memory": 1, "disk_size": 20, "traffic": 1, "available_generations": []string{"gen1", "gen2"}},
		{"id": 12, "slug": "cloud-s", "name": "Cloud S", "cpus": 2, "memory": 2, "disk_size": 40, "traffic": 2, "available_generations": []string{"gen1", "gen2"}},
		{"id": 13, "slug": "cloud-m", "name": "Cloud M", "cpus": 4, "memory": 8, "disk_size": 80, "traffic": 4, "available_generations": []string{"gen2"}},
		// The free tier: the platform attaches its read-only rule set to
		// every server created with it (see freeTierPackage).
		{"id": 10, "slug": freeTierPackage, "name": "Cloud V0", "cpus": 1, "memory": 1, "disk_size": 10, "traffic": 1, "available_generations": []string{"general-purpose"}},
	}
	f.storageProducts = []map[string]any{
		{"id": 21, "slug": "ssd", "name": "SSD storage", "type": "volume", "unit": "GB", "price": "0.0500", "min_size": 10, "max_size": 2000},
		{"id": 22, "slug": "hdd", "name": "HDD storage", "type": "volume", "unit": "GB", "price": "0.0200", "min_size": 50, "max_size": 10000},
	}
}

// freeTierPackage is the slug of the free-tier package.
const freeTierPackage = "cloudv-0"

func (f *Server) catalogRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cloud/images/{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, paginate(r, toAny(f.images)))
	})
	mux.HandleFunc("GET /api/cloud/server-packages/{$}", func(w http.ResponseWriter, r *http.Request) {
		gen := r.URL.Query().Get("generation")
		var out []map[string]any
		for _, p := range f.packages {
			if gen == "" || slices.Contains(p["available_generations"].([]string), gen) {
				out = append(out, p)
			}
		}
		writeJSON(w, http.StatusOK, paginate(r, toAny(out)))
	})
	mux.HandleFunc("GET /api/cloud/storage-products/{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, paginate(r, toAny(f.storageProducts)))
	})
}

func toAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
