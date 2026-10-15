// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package tfutil

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Known reports whether v is neither null nor unknown.
func Known(v interface {
	IsNull() bool
	IsUnknown() bool
}) bool {
	return !v.IsNull() && !v.IsUnknown()
}

// StringPtr returns nil for a null or unknown value.
func StringPtr(v types.String) *string {
	if !Known(v) {
		return nil
	}
	s := v.ValueString()
	return &s
}

// BoolPtr returns nil for a null or unknown value.
func BoolPtr(v types.Bool) *bool {
	if !Known(v) {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// Int32Ptr returns nil for a null or unknown value.
func Int32Ptr(v types.Int64) *int32 {
	if !Known(v) {
		return nil
	}
	n := int32(v.ValueInt64())
	return &n
}

// StringOrNull maps "" to null.
func StringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// SameText reports whether two strings are equal after trimming surrounding
// whitespace. The API trims SSH keys and similar text fields, so a trailing
// newline from file() must not show up as drift.
func SameText(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// KeepIfSameText returns prior when it is known and equal to current modulo
// surrounding whitespace, and current otherwise. It keeps the user's exact
// spelling in state while still detecting real drift.
func KeepIfSameText(prior types.String, current string) types.String {
	if Known(prior) && SameText(prior.ValueString(), current) {
		return prior
	}
	return types.StringValue(current)
}
