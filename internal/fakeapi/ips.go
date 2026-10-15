// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"fmt"
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
