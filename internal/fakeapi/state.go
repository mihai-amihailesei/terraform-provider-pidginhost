// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import "net/http"

// state holds the fake's resources, one map per resource type.
type state struct {
	sshKeys     map[int32]*sshKey
	ruleSets    map[int32]*ruleSet
	privateNets map[int32]*privateNetwork
}

func (f *Server) initState() {
	f.sshKeys = map[int32]*sshKey{}
	f.ruleSets = map[int32]*ruleSet{}
	f.privateNets = map[int32]*privateNetwork{}
}

func (f *Server) routes(mux *http.ServeMux) {
	f.catalogRoutes(mux)
	f.sshKeyRoutes(mux)
	f.firewallRoutes(mux)
	f.privateNetworkRoutes(mux)
}

// ruleSetInUse reports whether a server uses the rule set. Servers are
// added to the fake in a later file; until then nothing uses a set.
func (f *Server) ruleSetInUse(int32) bool { return false }

// hostnameOf returns the hostname of a server. Servers are added to the
// fake in a later file.
func (f *Server) hostnameOf(int32) string { return "" }
