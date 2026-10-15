// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	sdk "github.com/pidginhost/sdk-go"
)

// IsNotFound reports whether an SDK call failed with 404.
func IsNotFound(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusNotFound
}

// StatusCode returns the HTTP status of resp, or 0 when there is none.
func StatusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// ErrorDetail turns an SDK error into a readable message. The SDK's own error
// string is only the HTTP status line; the useful part (DRF's validation
// messages) is in the body, which this extracts.
func ErrorDetail(err error, resp *http.Response) string {
	if err == nil {
		return ""
	}
	var body []byte
	var gerr *sdk.GenericOpenAPIError
	if errors.As(err, &gerr) {
		body = gerr.Body()
	}
	var gval sdk.GenericOpenAPIError
	if body == nil && errors.As(err, &gval) {
		body = gval.Body()
	}

	msg := err.Error()
	if detail := formatBody(body); detail != "" {
		msg = fmt.Sprintf("%s: %s", msg, detail)
	}
	if resp != nil && resp.Request != nil {
		msg = fmt.Sprintf("%s %s: %s", resp.Request.Method, resp.Request.URL.Path, msg)
	}
	return msg
}

// formatBody renders a DRF error body. DRF answers either
// {"detail": "..."} or {"field": ["message", ...], ...}, or a list.
func formatBody(body []byte) string {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return ""
	}
	var obj map[string]any
	if json.Unmarshal(body, &obj) == nil {
		if d, ok := obj["detail"].(string); ok && len(obj) == 1 {
			return d
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", k, flatten(obj[k])))
		}
		return strings.Join(parts, "; ")
	}
	var list []any
	if json.Unmarshal(body, &list) == nil {
		return flatten(list)
	}
	s := string(body)
	if len(s) > 500 {
		s = s[:500] + "..."
	}
	return s
}

func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, flatten(e))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		b, _ := json.Marshal(t)
		return string(b)
	default:
		return fmt.Sprint(t)
	}
}
