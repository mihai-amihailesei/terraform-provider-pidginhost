// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this service.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{
		NewPrivateNetworkResource,
		NewPrivateNetworkAttachmentResource,
		NewIPv4Resource,
		NewIPv6Resource,
		NewIPv4AttachmentResource,
		NewIPv6AttachmentResource,
	}
}

// DataSources returns the data sources of this service.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
