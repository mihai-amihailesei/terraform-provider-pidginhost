// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package volume_test

import "regexp"

func regexpMust(s string) *regexp.Regexp { return regexp.MustCompile(s) }
