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
}

func (f *Server) initState() {
	f.sshKeys = map[int32]*sshKey{}
	f.ruleSets = map[int32]*ruleSet{}
	f.privateNets = map[int32]*privateNetwork{}
	f.servers = map[int32]*server{}
	f.ipv4s = map[int32]*publicIP{}
	f.ipv6s = map[int32]*publicIP{}
}

func (f *Server) routes(mux *http.ServeMux) {
	f.catalogRoutes(mux)
	f.sshKeyRoutes(mux)
	f.firewallRoutes(mux)
	f.privateNetworkRoutes(mux)
	f.serverRoutes(mux)
}

// Hooks filled in by resources added to the fake later.

func (f *Server) serverVolumes(int32) []any                          { return []any{} }
func (f *Server) serverFloatingIPs(int32) []any                      { return []any{} }
func (f *Server) onServerRemovedExtra(int32)                         {}
func (f *Server) createVolume(_ int32, _ string, _ int, _, _ string) {}
