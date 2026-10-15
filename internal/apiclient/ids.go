// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// ParseID converts a Terraform string ID into the int32 the SDK uses.
func ParseID(s string) (int32, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("%q is not a valid Pidgin Host ID (expected a positive integer)", s)
	}
	return int32(n), nil
}

// FormatID converts an SDK ID into a Terraform string ID.
func FormatID(id int32) string { return strconv.FormatInt(int64(id), 10) }

// JoinID builds a composite ID such as "12/34".
func JoinID(parts ...string) string { return strings.Join(parts, "/") }

// SplitID splits a composite ID into exactly n parts.
func SplitID(id string, n int, format string) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != n {
		return nil, fmt.Errorf("unexpected ID %q: expected the format %s", id, format)
	}
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("unexpected ID %q: expected the format %s", id, format)
		}
	}
	return parts, nil
}

// ListAll walks a paginated list endpoint. fetch returns the items of one
// page and whether a next page exists.
func ListAll[T any](ctx context.Context, fetch func(ctx context.Context, page int32) ([]T, bool, *http.Response, error)) ([]T, error) {
	var all []T
	for page := int32(1); ; page++ {
		items, more, resp, err := fetch(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("%s", ErrorDetail(err, resp))
		}
		all = append(all, items...)
		if !more || len(items) == 0 {
			return all, nil
		}
		if page > 1000 {
			return nil, fmt.Errorf("pagination did not terminate after %d pages", page)
		}
	}
}
