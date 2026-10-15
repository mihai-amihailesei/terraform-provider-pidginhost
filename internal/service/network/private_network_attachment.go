// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network

import (
	"context"
	"fmt"
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

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*privateNetworkAttachmentResource)(nil)
	_ resource.ResourceWithImportState = (*privateNetworkAttachmentResource)(nil)
)

type privateNetworkAttachmentResource struct{ tfutil.ResourceBase }

// NewPrivateNetworkAttachmentResource returns the
// pidginhost_private_network_attachment resource.
func NewPrivateNetworkAttachmentResource() resource.Resource {
	return &privateNetworkAttachmentResource{}
}

type privateNetworkAttachmentModel struct {
	ID               types.String   `tfsdk:"id"`
	PrivateNetworkID types.String   `tfsdk:"private_network_id"`
	ServerID         types.String   `tfsdk:"server_id"`
	Address          types.String   `tfsdk:"address"`
	Interface        types.String   `tfsdk:"interface"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func (r *privateNetworkAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_private_network_attachment"
}

func (r *privateNetworkAttachmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Connects a server to a private network (`add-server` / `remove-server`). " +
			"The API has no way to change a membership, so every argument forces a new attachment.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "`<private_network_id>/<server_id>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"private_network_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the private network.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"server_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the server.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"address": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Address of the server in the network. When omitted, the API assigns one.",
				Validators:          []validator.String{tfutil.IsIPv4()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown(),
				},
			},
			"interface": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Name of the server's interface in the network.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Delete: true}),
		},
	}
}

// membership looks the server up in the network. found is false when either
// is gone or the server is not a member.
//
// The add-server and remove-server endpoints identify the server by hostname
// (or private IP), not by ID, and PrivateNetwork.servers is typed as a list
// of free-form string maps. Membership is therefore read from the server's
// typed networks.private list, matching the network by its CIDR (address or
// slug).
func (r *privateNetworkAttachmentResource) membership(ctx context.Context, networkID, serverID int32) (
	net *sdk.PrivateNetwork, srv *sdk.ServerDetail, iface *sdk.ServerPrivateInterface, err error,
) {
	n, httpResp, err := r.Client.Cloud().CloudPrivateNetworksRetrieve(ctx, networkID).Execute()
	if apiclient.IsNotFound(httpResp) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading private network %d: %s", networkID, apiclient.ErrorDetail(err, httpResp))
	}
	s, httpResp, err := r.Client.Cloud().CloudServersRetrieve(ctx, serverID).Execute()
	if apiclient.IsNotFound(httpResp) {
		return n, nil, nil, nil
	}
	if err != nil {
		return n, nil, nil, fmt.Errorf("reading server %d: %s", serverID, apiclient.ErrorDetail(err, httpResp))
	}
	nets := s.GetNetworks()
	for _, p := range nets.GetPrivate() {
		if samePrefix(p.GetNetwork(), n.GetAddress()) || p.GetNetwork() == n.GetSlug() ||
			p.GetNetwork() == apiclient.FormatID(n.GetId()) {
			return n, s, &p, nil
		}
	}
	return n, s, nil, nil
}

func (r *privateNetworkAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan privateNetworkAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	networkID, err1 := apiclient.ParseID(plan.PrivateNetworkID.ValueString())
	serverID, err2 := apiclient.ParseID(plan.ServerID.ValueString())
	if err1 != nil || err2 != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprint(err1, err2))
		return
	}

	s, httpResp, err := r.Client.Cloud().CloudServersRetrieve(ctx, serverID).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading server", err, httpResp)
		return
	}
	body := sdk.NewPrivateNetworkAddHostRequest(s.GetHostname())
	if tfutil.Known(plan.Address) {
		body.SetAddress(plan.Address.ValueString())
	}
	if _, httpResp, err := r.Client.Cloud().CloudPrivateNetworksAddServerCreate(ctx, networkID).PrivateNetworkAddHostRequest(*body).Execute(); err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error adding server to private network", err, httpResp)
		return
	}

	// Assumption: the spec does not say whether add-server takes effect
	// immediately. Poll until the interface shows up on the server.
	var iface *sdk.ServerPrivateInterface
	err = apiclient.WaitFor(ctx, r.Client.PollInterval,
		fmt.Sprintf("server %d to appear in private network %d", serverID, networkID),
		func(ctx context.Context) (bool, string, error) {
			_, _, i, err := r.membership(ctx, networkID, serverID)
			if err != nil {
				return false, "", err
			}
			iface = i
			return i != nil, "not attached yet", nil
		})
	if err != nil {
		resp.Diagnostics.AddError("Server did not join the private network", err.Error())
		return
	}

	plan.ID = types.StringValue(apiclient.JoinID(plan.PrivateNetworkID.ValueString(), plan.ServerID.ValueString()))
	plan.Address = types.StringValue(iface.GetAddress())
	plan.Interface = types.StringValue(iface.GetInterface())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *privateNetworkAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state privateNetworkAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	networkID, err1 := apiclient.ParseID(state.PrivateNetworkID.ValueString())
	serverID, err2 := apiclient.ParseID(state.ServerID.ValueString())
	if err1 != nil || err2 != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprint(err1, err2))
		return
	}
	_, _, iface, err := r.membership(ctx, networkID, serverID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading private network attachment", err.Error())
		return
	}
	if iface == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Address = types.StringValue(iface.GetAddress())
	state.Interface = types.StringValue(iface.GetInterface())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *privateNetworkAttachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every argument forces replacement; there is nothing to update.
}

func (r *privateNetworkAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state privateNetworkAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	networkID, err1 := apiclient.ParseID(state.PrivateNetworkID.ValueString())
	serverID, err2 := apiclient.ParseID(state.ServerID.ValueString())
	if err1 != nil || err2 != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprint(err1, err2))
		return
	}
	n, s, iface, err := r.membership(ctx, networkID, serverID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading private network attachment", err.Error())
		return
	}
	if n == nil || s == nil || iface == nil {
		return // already gone
	}
	body := sdk.NewPrivateNetworkRemoveHostRequest(s.GetHostname())
	if _, httpResp, err := r.Client.Cloud().CloudPrivateNetworksRemoveServerCreate(ctx, networkID).PrivateNetworkRemoveHostRequest(*body).Execute(); err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error removing server from private network", err, httpResp)
		return
	}
	err = apiclient.WaitFor(ctx, r.Client.PollInterval,
		fmt.Sprintf("server %d to leave private network %d", serverID, networkID),
		func(ctx context.Context) (bool, string, error) {
			_, _, i, err := r.membership(ctx, networkID, serverID)
			return err == nil && i == nil, "still attached", err
		})
	if err != nil {
		resp.Diagnostics.AddError("Server did not leave the private network", err.Error())
	}
}

func (r *privateNetworkAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := apiclient.SplitID(req.ID, 2, "<private_network_id>/<server_id>")
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("private_network_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[1])...)
}
