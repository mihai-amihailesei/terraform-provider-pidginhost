// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"context"
	"net/http"
)

type bodyKey struct{}

func withBody(ctx context.Context, body map[string]any) context.Context {
	return context.WithValue(ctx, bodyKey{}, body)
}

func bodyOf(r *http.Request) map[string]any {
	b, _ := r.Context().Value(bodyKey{}).(map[string]any)
	if b == nil {
		b = map[string]any{}
	}
	return b
}
