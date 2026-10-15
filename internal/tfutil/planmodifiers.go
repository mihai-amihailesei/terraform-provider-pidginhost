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
		resp.RequiresReplace = !(req.StateValue.IsNull() && wasImported(ctx, req.Private))
	}, createOnlyDesc, createOnlyDesc)
}

// CreateOnlyBool is the bool variant of CreateOnlyString.
func CreateOnlyBool() planmodifier.Bool {
	return boolplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = !(req.StateValue.IsNull() && wasImported(ctx, req.Private))
	}, createOnlyDesc, createOnlyDesc)
}

// CreateOnlyInt64 is the int64 variant of CreateOnlyString.
func CreateOnlyInt64() planmodifier.Int64 {
	return int64planmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
		resp.RequiresReplace = !(req.StateValue.IsNull() && wasImported(ctx, req.Private))
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
