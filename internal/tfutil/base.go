// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package tfutil holds plumbing shared by every resource and data source:
// provider-data wiring, value conversions and custom plan modifiers.
package tfutil

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
)

// ResourceBase is embedded by every resource. It receives the API client
// from the provider.
type ResourceBase struct {
	Client *apiclient.Client
}

// Configure implements resource.ResourceWithConfigure.
func (b *ResourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*apiclient.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *apiclient.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	b.Client = c
}

// DataSourceBase is embedded by every data source.
type DataSourceBase struct {
	Client *apiclient.Client
}

// Configure implements datasource.DataSourceWithConfigure.
func (b *DataSourceBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*apiclient.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *apiclient.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	b.Client = c
}

// AddAPIError appends an error diagnostic for a failed SDK call.
func AddAPIError(d *diag.Diagnostics, summary string, err error, resp *http.Response) {
	d.AddError(summary, apiclient.ErrorDetail(err, resp))
}

// AddIDError appends an error diagnostic for an unparsable ID.
func AddIDError(d *diag.Diagnostics, err error) {
	d.AddError("Invalid ID", err.Error())
}
