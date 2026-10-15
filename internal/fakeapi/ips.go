// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"fmt"
	"net/http"
	"strconv"
)

type publicIP struct {
	ID         int32
	V6         bool
	Address    string
	Gateway    string
	Prefix     int
	ServerID   int32
	ReverseDNS string
}

func (ip *publicIP) json(f *Server) map[string]any {
	var sid any
	host := ""
	if ip.ServerID != 0 {
		sid = ip.ServerID
		host = f.hostnameOf(ip.ServerID)
	}
	return map[string]any{
		"id": ip.ID, "slug": ip.Address, "address": ip.Address, "gateway": ip.Gateway, "prefix": ip.Prefix,
		"attached": ip.ServerID != 0, "server": host, "server_id": sid,
	}
}

func (f *Server) ipStore(v6 bool) map[int32]*publicIP {
	if v6 {
		return f.ipv6s
	}
	return f.ipv4s
}

func (f *Server) allocateIP(v6 bool) *publicIP {
	id := f.id()
	ip := &publicIP{ID: id, V6: v6}
	if v6 {
		ip.Address, ip.Gateway, ip.Prefix = fmt.Sprintf("2001:db8::%x", id), "2001:db8::1", 64
	} else {
		ip.Address, ip.Gateway, ip.Prefix = fmt.Sprintf("192.0.2.%d", id%250+2), "192.0.2.1", 24
	}
	ip.ReverseDNS = fmt.Sprintf("ip-%d.fake.invalid", id)
	f.ipStore(v6)[id] = ip
	return ip
}

// findIP resolves an "ID or address" reference.
func (f *Server) findIP(v6 bool, ref string) *publicIP {
	for _, ip := range f.ipStore(v6) {
		if strconv.Itoa(int(ip.ID)) == ref || ip.Address == ref {
			return ip
		}
	}
	return nil
}

// serverPrimaryIPs returns the first attached IPv4 and IPv6 of a server.
func (f *Server) serverPrimaryIPs(sid int32) (string, string) {
	var v4, v6 string
	for _, id := range sortedKeys(f.ipv4s) {
		if ip := f.ipv4s[id]; ip.ServerID == sid && v4 == "" {
			v4 = ip.Address
		}
	}
	for _, id := range sortedKeys(f.ipv6s) {
		if ip := f.ipv6s[id]; ip.ServerID == sid && v6 == "" {
			v6 = ip.Address
		}
	}
	return v4, v6
}

// IPv4IDs returns the IDs of all IPv4 addresses.
func (f *Server) IPv4IDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.ipv4s)
}

// IPv6IDs returns the IDs of all IPv6 addresses.
func (f *Server) IPv6IDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.ipv6s)
}

// attachAtCreate applies the network options of a server create request.
func (f *Server) attachAtCreate(s *server, b map[string]any) (field, msg string) {
	if ref, ok := str(b, "public_ip"); ok {
		ip := f.findIP(false, ref)
		if ip == nil || ip.ServerID != 0 {
			return "public_ip", "IPv4 not found or already attached."
		}
		ip.ServerID = s.ID
	}
	if ref, ok := str(b, "public_ipv6"); ok {
		ip := f.findIP(true, ref)
		if ip == nil || ip.ServerID != 0 {
			return "public_ipv6", "IPv6 not found or already attached."
		}
		ip.ServerID = s.ID
	}
	if v, _ := boolean(b, "new_ipv4"); v {
		f.allocateIP(false).ServerID = s.ID
	}
	if v, _ := boolean(b, "new_ipv6"); v {
		f.allocateIP(true).ServerID = s.ID
	}
	v4, v6 := f.serverPrimaryIPs(s.ID)
	s.HasPublic = v4 != "" || v6 != ""
	if ack, _ := boolean(b, "no_network_acknowledged"); !s.HasPublic && !ack {
		// Undo any attachment made above before failing.
		f.onServerRemoved(s.ID)
		return "no_network_acknowledged", "The server has no public IP; set no_network_acknowledged to confirm."
	}
	if ref, ok := str(b, "private_network"); ok {
		n, _ := strconv.Atoi(ref)
		pn := f.privateNets[int32(n)]
		if pn == nil {
			return "private_network", "Private network not found."
		}
		addr, _ := str(b, "private_address")
		if addr == "" {
			addr = pn.nextAddress()
		}
		pn.Members[s.ID] = addr
	}
	if prod, ok := str(b, "extra_volume_product"); ok {
		size, _ := integer(b, "extra_volume_size")
		f.createVolume(s.ID, prod, size, s.Hostname+"-extra", s.Project)
	}
	return "", ""
}

// onServerRemoved releases what a deleted server held.
func (f *Server) onServerRemoved(sid int32) {
	for _, ip := range f.ipv4s {
		if ip.ServerID == sid {
			ip.ServerID = 0
		}
	}
	for _, ip := range f.ipv6s {
		if ip.ServerID == sid {
			ip.ServerID = 0
		}
	}
	for _, pn := range f.privateNets {
		delete(pn.Members, sid)
	}
	f.onServerRemovedExtra(sid)
}

// SetReverseDNS changes a PTR name out of band.
func (f *Server) SetReverseDNS(v6 bool, id int32, name string) {
	f.Do(func() { f.ipStore(v6)[id].ReverseDNS = name })
}

// DetachIP detaches an address out of band.
func (f *Server) DetachIP(v6 bool, id int32) {
	f.Do(func() { f.ipStore(v6)[id].ServerID = 0 })
}

func (f *Server) ipRoutes(mux *http.ServeMux) {
	for _, fam := range []struct {
		name string
		v6   bool
	}{{"ipv4", false}, {"ipv6", true}} {
		base := "/api/cloud/" + fam.name + "/"
		with := func(w http.ResponseWriter, r *http.Request, fn func(ip *publicIP)) {
			id, ok := pathID(r, "id")
			f.mu.Lock()
			defer f.mu.Unlock()
			ip := f.ipStore(fam.v6)[id]
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
			for _, id := range sortedKeys(f.ipStore(fam.v6)) {
				items = append(items, f.ipStore(fam.v6)[id].json(f))
			}
			writeJSON(w, http.StatusOK, paginate(r, items))
		})
		mux.HandleFunc("POST "+base+"{$}", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			writeJSON(w, http.StatusCreated, f.allocateIP(fam.v6).json(f))
		})
		mux.HandleFunc("GET "+base+"{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *publicIP) { writeJSON(w, http.StatusOK, ip.json(f)) })
		})
		mux.HandleFunc("DELETE "+base+"{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *publicIP) {
				if ip.ServerID != 0 {
					badRequest(w, "detail", "Detach the address before deleting it.")
					return
				}
				delete(f.ipStore(fam.v6), ip.ID)
				w.WriteHeader(http.StatusNoContent)
			})
		})
		mux.HandleFunc("POST "+base+"{id}/detach/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *publicIP) {
				detached := ip.ServerID != 0
				ip.ServerID = 0
				writeJSON(w, http.StatusOK, map[string]any{"detached": detached})
			})
		})
		mux.HandleFunc("GET "+base+"{id}/rdns/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *publicIP) { writeJSON(w, http.StatusOK, map[string]any{"reverse_dns": ip.ReverseDNS}) })
		})
		mux.HandleFunc("POST "+base+"{id}/rdns/{$}", func(w http.ResponseWriter, r *http.Request) {
			with(w, r, func(ip *publicIP) {
				name, _ := str(bodyOf(r), "reverse_dns")
				if name == "" {
					badRequest(w, "reverse_dns", "This field may not be blank.")
					return
				}
				ip.ReverseDNS = name
				writeJSON(w, http.StatusOK, map[string]any{"reverse_dns": name})
			})
		})
		mux.HandleFunc("POST /api/cloud/servers/{id}/attach-"+fam.name+"/{$}", func(w http.ResponseWriter, r *http.Request) {
			f.withServer(w, r, func(s *server) {
				b := bodyOf(r)
				ref, _ := str(b, fam.name)
				ip := f.findIP(fam.v6, ref)
				if ip == nil {
					badRequest(w, fam.name, "Address not found.")
					return
				}
				if ip.ServerID != 0 && ip.ServerID != s.ID {
					badRequest(w, fam.name, "The address is attached to another server.")
					return
				}
				ip.ServerID = s.ID
				s.HasPublic = true
				reboot, _ := boolean(b, "reboot")
				writeJSON(w, http.StatusOK, map[string]any{"attached": true, "reboot_required": true, "rebooted": reboot})
			})
		})
	}
}

// DetachAllIPs detaches every address of one family from a server, out of band.
func (f *Server) DetachAllIPs(v6 bool, serverID int32) {
	f.Do(func() {
		for _, ip := range f.ipStore(v6) {
			if ip.ServerID == serverID {
				ip.ServerID = 0
			}
		}
	})
}
