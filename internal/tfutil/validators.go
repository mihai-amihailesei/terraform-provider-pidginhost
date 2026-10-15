// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package tfutil

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type stringFuncValidator struct {
	desc  string
	check func(string) error
}

func (v stringFuncValidator) Description(context.Context) string         { return v.desc }
func (v stringFuncValidator) MarkdownDescription(context.Context) string { return v.desc }

func (v stringFuncValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !Known(req.ConfigValue) {
		return
	}
	if err := v.check(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value", err.Error())
	}
}

// IsCIDR validates an IPv4 or IPv6 prefix such as 10.0.0.0/24.
func IsCIDR() validator.String {
	return stringFuncValidator{desc: "value must be a CIDR prefix such as 10.0.0.0/24", check: func(s string) error {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return fmt.Errorf("%q is not a CIDR prefix such as 10.0.0.0/24", s)
		}
		if p.Masked() != p {
			return fmt.Errorf("%q has host bits set; did you mean %s?", s, p.Masked())
		}
		return nil
	}}
}

// IsIPv4 validates an IPv4 address.
func IsIPv4() validator.String {
	return stringFuncValidator{desc: "value must be an IPv4 address", check: func(s string) error {
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() {
			return fmt.Errorf("%q is not an IPv4 address", s)
		}
		return nil
	}}
}

// IsIP validates an IPv4 or IPv6 address.
func IsIP() validator.String {
	return stringFuncValidator{desc: "value must be an IP address", check: func(s string) error {
		if _, err := netip.ParseAddr(s); err != nil {
			return fmt.Errorf("%q is not an IP address", s)
		}
		return nil
	}}
}

// IsID validates a numeric Pidgin Host ID.
func IsID() validator.String {
	return stringFuncValidator{desc: "value must be a numeric ID", check: func(s string) error {
		for _, r := range s {
			if r < '0' || r > '9' {
				return fmt.Errorf("%q is not a numeric ID", s)
			}
		}
		if s == "" {
			return fmt.Errorf("the ID must not be empty")
		}
		return nil
	}}
}
