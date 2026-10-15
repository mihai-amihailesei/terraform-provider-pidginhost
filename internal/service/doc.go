// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package service groups resources and data sources by API area. Each
// sub-package exposes Resources() and DataSources(), which the provider
// registers. To support a new area of the API (Kubernetes, DNS, ...), add a
// sub-package and register it in internal/provider/provider.go.
package service
