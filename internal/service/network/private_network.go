// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package network implements private networks, public IPv4/IPv6 addresses,
// floating IPs and their attachments to servers.
package network

import (
	"context"
	"net/netip"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*privateNetworkResource)(nil)
	_ resource.ResourceWithImportState = (*privateNetworkResource)(nil)
)

type privateNetworkResource struct{ tfutil.ResourceBase }

// NewPrivateNetworkResource returns the pidginhost_private_network resource.
func NewPrivateNetworkResource() resource.Resource { return &privateNetworkResource{} }

type privateNetworkModel struct {
	ID          types.String   `tfsdk:"id"`
	Address     types.String   `tfsdk:"address"`
	Gateway     types.String   `tfsdk:"gateway"`
	Slug        types.String   `tfsdk:"slug"`
	Provisioned types.Bool     `tfsdk:"provisioned"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func (r *privateNetworkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_private_network"
}

func (r *privateNetworkResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A private network. Connect servers to it with `pidginhost_private_network_attachment`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Private network ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Address range in CIDR format, for example `10.10.0.0/24`. Changing it replaces the network.",
				Validators:          []validator.String{tfutil.IsCIDR()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"gateway": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Gateway address inside the network. Can be changed or removed in place.",
				Validators:          []validator.String{tfutil.IsIP()},
			},
			"slug": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Slug of the network (its CIDR, as returned by the API).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"provisioned": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the network has been provisioned on the platform.",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func samePrefix(a, b string) bool {
	if a == b {
		return true
	}
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	return errA == nil && errB == nil && pa.Masked() == pb.Masked()
}

func applyPrivateNetwork(m *privateNetworkModel, n *sdk.PrivateNetwork) {
	m.ID = types.StringValue(apiclient.FormatID(n.GetId()))
	// Keep the configured spelling when the API returns the same prefix in
	// another form, so normalisation never forces a replacement.
	if !samePrefix(m.Address.ValueString(), n.GetAddress()) {
		m.Address = types.StringValue(n.GetAddress())
	}
	m.Gateway = tfutil.StringOrNull(n.GetGateway())
	m.Slug = types.StringValue(n.GetSlug())
	m.Provisioned = types.BoolValue(n.GetProvisioned())
}

// Create adds the network. The spec answers 201 with the network and
// a "provisioned" flag. It does not say whether provisioning is
// asynchronous or what makes it complete, so Create does not wait for
// provisioned=true; the flag is exported for users who need it.
func (r *privateNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan privateNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewPrivateNetworkRequest(plan.Address.ValueString())
	if tfutil.Known(plan.Gateway) {
		body.SetGateway(plan.Gateway.ValueString())
	}
	n, httpResp, err := r.Client.Cloud().CloudPrivateNetworksCreate(ctx).PrivateNetworkRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating private network", err, httpResp)
		return
	}
	applyPrivateNetwork(&plan, n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *privateNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state privateNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	n, httpResp, err := r.Client.Cloud().CloudPrivateNetworksRetrieve(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading private network", err, httpResp)
		return
	}
	applyPrivateNetwork(&state, n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *privateNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan privateNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(plan.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	body := sdk.NewPatchedPrivateNetworkUpdateRequest()
	if tfutil.Known(plan.Gateway) {
		body.SetGateway(plan.Gateway.ValueString())
	} else {
		// Send an explicit null: omitting the field would leave the old
		// gateway in place.
		body.SetGatewayNil()
	}
	n, httpResp, err := r.Client.Cloud().CloudPrivateNetworksPartialUpdate(ctx, id).PatchedPrivateNetworkUpdateRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error updating private network", err, httpResp)
		return
	}
	applyPrivateNetwork(&plan, n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *privateNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state privateNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	httpResp, err := r.Client.Cloud().CloudPrivateNetworksDestroy(ctx, id).Execute()
	if err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting private network", err, httpResp)
	}
}

func (r *privateNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
