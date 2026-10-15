// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package tfutil

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestValidators(t *testing.T) {
	cases := []struct {
		v     validator.String
		value string
		ok    bool
	}{
		{IsCIDR(), "10.0.0.0/24", true},
		{IsCIDR(), "fd00::/64", true},
		{IsCIDR(), "10.0.0.1/24", false},
		{IsCIDR(), "10.0.0.0", false},
		{IsIPv4(), "10.0.0.1", true},
		{IsIPv4(), "fd00::1", false},
		{IsIP(), "fd00::1", true},
		{IsIP(), "nope", false},
		{IsID(), "123", true},
		{IsID(), "12a", false},
		{IsID(), "", false},
	}
	for _, c := range cases {
		resp := &validator.StringResponse{}
		c.v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: types.StringValue(c.value)}, resp)
		if got := !resp.Diagnostics.HasError(); got != c.ok {
			t.Errorf("%s(%q) ok = %v, want %v", c.v.Description(context.Background()), c.value, got, c.ok)
		}
	}
}

func TestKeepIfSameText(t *testing.T) {
	prior := types.StringValue("ssh-ed25519 AAAA\n")
	if got := KeepIfSameText(prior, "ssh-ed25519 AAAA"); got != prior {
		t.Errorf("expected prior value to be kept, got %q", got.ValueString())
	}
	if got := KeepIfSameText(prior, "ssh-ed25519 BBBB"); got.ValueString() != "ssh-ed25519 BBBB" {
		t.Errorf("expected drift to be reported, got %q", got.ValueString())
	}
	if got := KeepIfSameText(types.StringNull(), "x"); got.ValueString() != "x" {
		t.Errorf("expected API value for null prior, got %q", got.ValueString())
	}
}
