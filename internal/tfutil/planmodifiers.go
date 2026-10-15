// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package tfutil

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
)

// privateKeyImported is set in private state by ImportState. It lets
// create-only attributes that the API never returns (passwords, user_data,
// the IP used at creation, ...) be written into the configuration of an
// imported resource without forcing it to be replaced.
const privateKeyImported = "imported"

// MarkImported flags a freshly imported resource in its private state.
// Call it from ImportState.
func MarkImported(ctx context.Context, resp *resource.ImportStateResponse) {
	if resp.Private == nil {
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateKeyImported, []byte("true"))...)
}

// ClearImported removes the import flag once an update has stored the
// configured values, so later changes to create-only attributes replace the
// resource as usual. Call it at the end of a successful Update.
func ClearImported(ctx context.Context, resp *resource.UpdateResponse) {
	if resp.Private == nil {
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateKeyImported, nil)...)
}

// WasImported reports whether the resource was imported and has not been
// updated since.
func WasImported(ctx context.Context, p privateGetter) bool { return wasImported(ctx, p) }

type privateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

func wasImported(ctx context.Context, p privateGetter) bool {
	if p == nil {
		return false
	}
	v, _ := p.GetKey(ctx, privateKeyImported)
	return string(v) == "true"
}

const createOnlyDesc = "Changing this value after creation replaces the resource. " +
	"After `terraform import` the value is unknown to the provider, so setting it the first time does not trigger a replacement."

// CreateOnlyString forces replacement when the value changes, except when
// the prior state value is null because the resource was imported.
func CreateOnlyString() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = !req.StateValue.IsNull() || !wasImported(ctx, req.Private)
	}, createOnlyDesc, createOnlyDesc)
}

// CreateOnlyBool is the bool variant of CreateOnlyString.
func CreateOnlyBool() planmodifier.Bool {
	return boolplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = !req.StateValue.IsNull() || !wasImported(ctx, req.Private)
	}, createOnlyDesc, createOnlyDesc)
}

// CreateOnlyInt64 is the int64 variant of CreateOnlyString.
func CreateOnlyInt64() planmodifier.Int64 {
	return int64planmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = !req.StateValue.IsNull() || !wasImported(ctx, req.Private)
	}, createOnlyDesc, createOnlyDesc)
}

// RequiresReplaceIfDecreased forces replacement when an integer goes down,
// for sizes that can only grow in place.
func RequiresReplaceIfDecreased() planmodifier.Int64 {
	const d = "Increasing this value updates the resource in place; decreasing it replaces the resource."
	return int64planmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
		if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
			return
		}
		resp.RequiresReplace = req.PlanValue.ValueInt64() < req.StateValue.ValueInt64()
	}, d, d)
}

// RequiresReplaceUnlessEquivalent forces replacement when a string changes,
// unless same reports that the old and new values denote the same thing
// (for example a short hostname and its FQDN), or the resource was just
// imported, in which case the state holds the API's spelling and any
// difference is assumed to be spelling only.
func RequiresReplaceUnlessEquivalent(same func(a, b string) bool, desc string) planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		if req.StateValue.IsNull() || req.PlanValue.IsUnknown() || req.PlanValue.IsNull() {
			return
		}
		if wasImported(ctx, req.Private) || same(req.StateValue.ValueString(), req.PlanValue.ValueString()) {
			return
		}
		resp.RequiresReplace = true
	}, desc, desc)
}
