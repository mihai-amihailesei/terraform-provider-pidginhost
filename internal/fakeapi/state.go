// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import "net/http"

// state holds the fake's resources, one map per resource type.
type state struct{}

func (f *Server) initState() {}

func (f *Server) routes(mux *http.ServeMux) {
	f.catalogRoutes(mux)
}
