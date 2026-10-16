// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package server

import (
	"testing"

	sdk "github.com/pidginhost/sdk-go"
)

func TestMatchRulesSetName(t *testing.T) {
	set := func(id int32, name string, readOnly bool) sdk.FirewallRulesSet {
		var s sdk.FirewallRulesSet
		s.SetId(id)
		s.SetName(name)
		s.SetReadOnly(readOnly)
		return s
	}
	sets := []sdk.FirewallRulesSet{
		set(767, "Free tier FW", true),
		set(10, "web", false),
		set(11, "Dup", false),
		set(12, "dup", false),
		set(13, "Same", false),
		set(14, "SAME", false),
	}
	for _, tc := range []struct {
		raw, prior, want string
	}{
		{raw: "free tier fw", want: "767"}, // the live API's spelling of a read-only set
		{raw: "FREE TIER FW", prior: "1", want: "767"},
		{raw: "web", want: "10"},
		{raw: "dup", want: "12"},              // ambiguous ignoring case: exact case wins
		{raw: "dup", prior: "11", want: "11"}, // the ID already in state wins
		{raw: "same", want: "same"},           // ambiguous, no exact match: kept raw
		{raw: "missing", prior: "10", want: "missing"},
	} {
		if got := matchRulesSetName(sets, tc.raw, tc.prior); got != tc.want {
			t.Errorf("matchRulesSetName(%q, prior %q) = %q, want %q", tc.raw, tc.prior, got, tc.want)
		}
	}
}
