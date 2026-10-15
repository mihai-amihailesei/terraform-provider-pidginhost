// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package server

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this service.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{}
}

// DataSources returns the data sources of this service.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
