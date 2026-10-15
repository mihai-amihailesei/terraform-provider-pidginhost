// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import "net/http"

// state holds the fake's resources, one map per resource type.
type state struct {
	sshKeys     map[int32]*sshKey
	ruleSets    map[int32]*ruleSet
	privateNets map[int32]*privateNetwork
	servers     map[int32]*server
	ipv4s       map[int32]*publicIP
	ipv6s       map[int32]*publicIP
	floatingV4  map[int32]*floatingIP
	floatingV6  map[int32]*floatingIP
	volumes     map[int32]*volume
}

func (f *Server) initState() {
	f.sshKeys = map[int32]*sshKey{}
	f.ruleSets = map[int32]*ruleSet{}
	f.privateNets = map[int32]*privateNetwork{}
	f.servers = map[int32]*server{}
	f.ipv4s = map[int32]*publicIP{}
	f.ipv6s = map[int32]*publicIP{}
	f.floatingV4 = map[int32]*floatingIP{}
	f.floatingV6 = map[int32]*floatingIP{}
	f.volumes = map[int32]*volume{}
}

func (f *Server) routes(mux *http.ServeMux) {
	f.catalogRoutes(mux)
	f.sshKeyRoutes(mux)
	f.firewallRoutes(mux)
	f.privateNetworkRoutes(mux)
	f.serverRoutes(mux)
	f.ipRoutes(mux)
	f.floatingRoutes(mux)
	f.volumeRoutes(mux)
}

// onServerRemovedExtra releases floating IP authorizations and volumes of a
// deleted server.
func (f *Server) onServerRemovedExtra(sid int32) {
	for _, store := range []map[int32]*floatingIP{f.floatingV4, f.floatingV6} {
		for _, ip := range store {
			delete(ip.Authorized, sid)
		}
	}
	// Volumes of a deleted server are detached, not deleted.
	for _, v := range f.volumes {
		if v.ServerID == sid {
			v.ServerID = 0
		}
	}
}
