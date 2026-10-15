// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"fmt"
	"net/http"
	"net/netip"
)

type privateNetwork struct {
	ID      int32
	Address string
	Gateway *string
	reads   int
	// Members maps server ID to its address in the network.
	Members map[int32]string
}

func (n *privateNetwork) json(f *Server) map[string]any {
	servers := []any{}
	for _, sid := range sortedKeys(n.Members) {
		servers = append(servers, map[string]any{"hostname": f.hostnameOf(sid), "address": n.Members[sid]})
	}
	var gw any
	if n.Gateway != nil {
		gw = *n.Gateway
	}
	return map[string]any{
		"id": n.ID, "slug": n.Address, "address": n.Address, "gateway": gw,
		// Provisioning completes after the first read.
		"provisioned": n.reads > 0, "servers": servers,
	}
}

// PrivateNetworkIDs returns the IDs of all private networks.
func (f *Server) PrivateNetworkIDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.privateNets)
}

// SetPrivateNetworkGateway changes a gateway out of band.
func (f *Server) SetPrivateNetworkGateway(id int32, gw string) {
	f.Do(func() { f.privateNets[id].Gateway = &gw })
}

func (f *Server) privateNetworkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cloud/private-networks/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []any{}
		for _, id := range sortedKeys(f.privateNets) {
			items = append(items, f.privateNets[id].json(f))
		}
		writeJSON(w, http.StatusOK, paginate(r, items))
	})
	mux.HandleFunc("POST /api/cloud/private-networks/{$}", func(w http.ResponseWriter, r *http.Request) {
		b := bodyOf(r)
		addr, _ := str(b, "address")
		if _, err := netip.ParsePrefix(addr); err != nil {
			badRequest(w, "address", "Enter a valid CIDR.")
			return
		}
		n := &privateNetwork{Address: addr, Members: map[int32]string{}}
		if gw, ok := str(b, "gateway"); ok {
			n.Gateway = &gw
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		n.ID = f.id()
		f.privateNets[n.ID] = n
		writeJSON(w, http.StatusCreated, n.json(f))
	})
	mux.HandleFunc("GET /api/cloud/private-networks/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withPrivateNetwork(w, r, func(n *privateNetwork) {
			writeJSON(w, http.StatusOK, n.json(f))
			n.reads++
		})
	})
	mux.HandleFunc("PATCH /api/cloud/private-networks/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withPrivateNetwork(w, r, func(n *privateNetwork) {
			b := bodyOf(r)
			if v, present := b["gateway"]; present {
				if v == nil {
					n.Gateway = nil
				} else if s, ok := v.(string); ok {
					n.Gateway = &s
				}
			}
			writeJSON(w, http.StatusOK, n.json(f))
		})
	})
	mux.HandleFunc("DELETE /api/cloud/private-networks/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withPrivateNetwork(w, r, func(n *privateNetwork) {
			if len(n.Members) > 0 {
				badRequest(w, "detail", "Remove all servers from the network first.")
				return
			}
			delete(f.privateNets, n.ID)
			w.WriteHeader(http.StatusNoContent)
		})
	})
}

func (f *Server) withPrivateNetwork(w http.ResponseWriter, r *http.Request, fn func(*privateNetwork)) {
	id, ok := pathID(r, "id")
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.privateNets[id]
	if !ok || n == nil {
		notFound(w)
		return
	}
	fn(n)
}

// nextAddress returns the first free host address in the network.
func (n *privateNetwork) nextAddress() string {
	p, err := netip.ParsePrefix(n.Address)
	if err != nil {
		return ""
	}
	used := map[string]bool{}
	for _, a := range n.Members {
		used[a] = true
	}
	if n.Gateway != nil {
		used[*n.Gateway] = true
	}
	for a := p.Addr().Next(); p.Contains(a); a = a.Next() {
		if !used[a.String()] {
			return a.String()
		}
	}
	return ""
}

// serverPrivateInterfaces lists a server's private interfaces in the shape
// of ServerPrivateInterface.
func (f *Server) serverPrivateInterfaces(sid int32) []any {
	out := []any{}
	i := 0
	for _, id := range sortedKeys(f.privateNets) {
		pn := f.privateNets[id]
		if addr, ok := pn.Members[sid]; ok {
			out = append(out, map[string]any{"interface": fmt.Sprintf("eth%d", i+1), "address": addr, "network": pn.Address})
			i++
		}
	}
	return out
}
