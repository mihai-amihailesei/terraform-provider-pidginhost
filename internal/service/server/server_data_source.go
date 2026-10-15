// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package server

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

type serverDataSource struct{ tfutil.DataSourceBase }

// NewServerDataSource returns the pidginhost_server data source.
func NewServerDataSource() datasource.DataSource { return &serverDataSource{} }

type serverDataModel struct {
	ID                types.String `tfsdk:"id"`
	Hostname          types.String `tfsdk:"hostname"`
	ImageName         types.String `tfsdk:"image_name"`
	Package           types.String `tfsdk:"package"`
	Generation        types.String `tfsdk:"generation"`
	Project           types.String `tfsdk:"project"`
	Status            types.String `tfsdk:"status"`
	PowerState        types.String `tfsdk:"power_state"`
	CPUs              types.Int64  `tfsdk:"cpus"`
	Memory            types.Int64  `tfsdk:"memory"`
	DiskSize          types.Int64  `tfsdk:"disk_size"`
	Username          types.String `tfsdk:"username"`
	IPv4Address       types.String `tfsdk:"ipv4_address"`
	IPv6Address       types.String `tfsdk:"ipv6_address"`
	PrivateNetworks   types.List   `tfsdk:"private_networks"`
	DestroyProtection types.Bool   `tfsdk:"destroy_protection"`
	HAEnabled         types.Bool   `tfsdk:"ha_enabled"`
}

func (d *serverDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (d *serverDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing server by `id` or `hostname` (exactly one).",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Server ID."},
			"hostname":     schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Hostname. It must match exactly one server (case-insensitive)."},
			"image_name":   computed("The installed image, as reported by the API."),
			"package":      computed("Server package, as reported by the API."),
			"generation":   computed("Hardware generation."),
			"project":      computed("Project label."),
			"status":       computed("Lifecycle status (`active`, `provisioning`, ...)."),
			"power_state":  computed("Power state (`running` or `stopped`), or null if the API did not report it."),
			"username":     computed("Default login user."),
			"ipv4_address": computed("Primary public IPv4 address, if any."),
			"ipv6_address": computed("Primary public IPv6 address, if any."),
			"cpus":         schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of vCPUs."},
			"memory":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Memory, as reported by the API."},
			"disk_size":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Boot disk size, as reported by the API."},
			"private_networks": schema.ListNestedAttribute{
				Computed: true, MarkdownDescription: "Private network interfaces.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"network":   computed("Network, as reported by the API."),
					"address":   computed("Address of the server in the network."),
					"interface": computed("Interface name."),
				}},
			},
			"destroy_protection": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether destroy protection is enabled."},
			"ha_enabled":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether high availability is enabled."},
		},
	}
}

func (d *serverDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("hostname")),
	}
}

func (d *serverDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg serverDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var id int32
	var err error
	if tfutil.Known(cfg.ID) {
		id, err = apiclient.ParseID(cfg.ID.ValueString())
	} else {
		id, err = findServerByHostname(ctx, d.Client, cfg.Hostname.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Server not found", err.Error())
		return
	}

	s, httpResp, err := d.Client.Cloud().CloudServersRetrieve(ctx, id).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading server", err, httpResp)
		return
	}

	// Reuse the resource's mapping for the shared attributes.
	var m serverModel
	applyDetail(ctx, &m, s, &resp.Diagnostics)

	cfg.ID = m.ID
	cfg.Hostname = types.StringValue(s.GetHostname())
	cfg.ImageName = m.ImageName
	cfg.Package = types.StringValue(s.GetPackage())
	cfg.Generation = types.StringValue(s.GetGeneration())
	cfg.Project = m.Project
	cfg.Status = m.Status
	cfg.CPUs = m.CPUs
	cfg.Memory = m.Memory
	cfg.DiskSize = m.DiskSize
	cfg.Username = m.Username
	cfg.IPv4Address = m.IPv4Address
	cfg.IPv6Address = m.IPv6Address
	cfg.PrivateNetworks = m.PrivateNetworks
	cfg.DestroyProtection = m.DestroyProtection
	cfg.HAEnabled = m.EnableHA
	cfg.PowerState = types.StringNull()
	if p, err := powerState(ctx, d.Client, id, s); err == nil {
		cfg.PowerState = types.StringValue(p)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
