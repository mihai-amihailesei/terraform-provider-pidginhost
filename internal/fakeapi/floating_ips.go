// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"fmt"
	"net/http"
)

type floatingIP struct {
	ID         int32
	V6         bool
	Address    string
	Label      string
	ReverseDNS string
	// Authorized maps server ID to the authorization row ID.
	Authorized map[int32]int32
}

func (ip *floatingIP) json() map[string]any {
	gw, prefix := "198.51.100.1", 32
	if ip.V6 {
		gw, prefix = "2001:db8:f::1", 128
	}
	return map[string]any{
		"id": ip.ID, "address": ip.Address, "gateway": gw, "prefix": prefix, "reverse_dns": ip.ReverseDNS,
		"label": ip.Label, "authorized_vm_count": len(ip.Authorized), "created_at": "2026-01-02T03:04:05Z",
	}
}

func (f *Server) floatingStore(v6 bool) map[int32]*floatingIP {
	if v6 {
		return f.floatingV6
	}
	return f.floatingV4
}

// FloatingIPIDs returns the IDs of the floating IPs of one family.
func (f *Server) FloatingIPIDs(v6 bool) []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.floatingStore(v6))
}

// Unauthorize removes every authorization of a floating IP out of band.
func (f *Server) Unauthorize(v6 bool, id int32) {
	f.Do(func() { f.floatingStore(v6)[id].Authorized = map[int32]int32{} })
}

func (f *Server) serverFloatingIPs(sid int32) []any {
	out := []any{}
	for _, store := range []map[int32]*floatingIP{f.floatingV4, f.floatingV6} {
		for _, id := range sortedKeys(store) {
			ip := store[id]
			if _, ok := ip.Authorized[sid]; ok {
				version := "ipv4"
				if ip.V6 {
					version = "ipv6"
				}
				out = append(out, map[string]any{"id": ip.ID, "version": version, "address": ip.Address,
					"label": ip.Label, "reverse_dns": ip.ReverseDNS})
			}
		}
	}
	return out
}

func (f *Server) floatingRoutes(mux *http.ServeMux) {
	for _, fam := range []struct {
		name string
		v6   bool
	}{{"floating-ipv4", false}, {"floating-ipv6", true}} {
		fam := fam
		base := "/api/cloud/" + fam.name + "/"
		with := func(w http.ResponseWriter, r *http.Request, fn func(ip *floatingIP)) {
			id, ok := pathID(r, "id")
			f.mu.Lock()
			defer f.mu.Unlock()
			ip := f.floatingStore(fam.v6)[id]
			if !ok || ip == nil {
				notFound(w)
				return
			}
			fn(ip)
		}
		mux.HandleFunc("GET "+base+"{$}", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			items := []any{}
			for _, id := range sortedKeys(f.floatingStore(fam.v6)) {
				items = append(items, f.floatingStore(fam.v6)[id].json())
			}
			writeJSON(w, http.StatusOK, paginate(r, items))
		})
		mux.HandleFunc("POST "+base+"{$}", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			id := f.id()
			ip := &floatingIP{ID: id, V6: fam.v6, Authorized: map[int32]int32{}}
			ip.Label, _ = str(bodyOf(r), "label")
			if fam.v6 {
				ip.Address = fmt.Sprintf("2001:db8:f::%x", id)
			} else {
				ip.Address = fmt.Sprintf("198.51.100.%d", id%250+2)
			}
			ip.ReverseDNS = fmt.Sprintf("float-%d.fake.invalid", id)
			f.floatingStore(fam.v6)[id] = ip
			writeJSON(w, http.StatusCreated, ip.json())
		})
		mux.HandleFunc("GET "+base+"{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) { writeJSON(w, http.StatusOK, ip.json()) })
		})
		mux.HandleFunc("DELETE "+base+"{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) {
				delete(f.floatingStore(fam.v6), ip.ID)
				w.WriteHeader(http.StatusNoContent)
			})
		})
		mux.HandleFunc("GET "+base+"{id}/rdns/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) { writeJSON(w, http.StatusOK, map[string]any{"reverse_dns": ip.ReverseDNS}) })
		})
		mux.HandleFunc("POST "+base+"{id}/rdns/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) {
				name, _ := str(bodyOf(r), "reverse_dns")
				if name == "" {
					badRequest(w, "reverse_dns", "This field may not be blank.")
					return
				}
				ip.ReverseDNS = name
				writeJSON(w, http.StatusOK, map[string]any{"reverse_dns": name})
			})
		})
		mux.HandleFunc("GET "+base+"{id}/authorizations/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) {
				items := []any{}
				for _, sid := range sortedKeys(ip.Authorized) {
					items = append(items, map[string]any{"id": ip.Authorized[sid], "server_id": sid,
						"server_hostname": f.hostnameOf(sid), "created_at": "2026-01-02T03:04:05Z"})
				}
				writeJSON(w, http.StatusOK, paginate(r, items))
			})
		})
		mux.HandleFunc("POST "+base+"{id}/authorize/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) {
				sid, _ := integer(bodyOf(r), "server_id")
				if f.servers[int32(sid)] == nil {
					badRequest(w, "server_id", "Server not found.")
					return
				}
				ip.Authorized[int32(sid)] = f.id()
				writeJSON(w, http.StatusOK, map[string]any{"authorized": true})
			})
		})
		mux.HandleFunc("POST "+base+"{id}/unauthorize/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *floatingIP) {
				sid, _ := integer(bodyOf(r), "server_id")
				_, was := ip.Authorized[int32(sid)]
				delete(ip.Authorized, int32(sid))
				writeJSON(w, http.StatusOK, map[string]any{"unauthorized": was})
			})
		})
	}
}
