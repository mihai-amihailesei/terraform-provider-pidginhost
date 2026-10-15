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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*ipAttachmentResource)(nil)
	_ resource.ResourceWithImportState = (*ipAttachmentResource)(nil)
)

type ipAttachmentResource struct {
	tfutil.ResourceBase
	family ipFamily
}

// NewIPv4AttachmentResource returns the pidginhost_ipv4_attachment resource.
func NewIPv4AttachmentResource() resource.Resource { return &ipAttachmentResource{family: ipv4Family} }

// NewIPv6AttachmentResource returns the pidginhost_ipv6_attachment resource.
func NewIPv6AttachmentResource() resource.Resource { return &ipAttachmentResource{family: ipv6Family} }

type ipAttachmentModel struct {
	ID             types.String   `tfsdk:"id"`
	ServerID       types.String   `tfsdk:"server_id"`
	AddressID      types.String   `tfsdk:"address_id"`
	Reboot         types.Bool     `tfsdk:"reboot"`
	RebootRequired types.Bool     `tfsdk:"reboot_required"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *ipAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.family.name + "_attachment"
}

func (r *ipAttachmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf("Attaches a `pidginhost_%[1]s` address to a server (`attach-%[1]s`); "+
			"destroying it detaches the address (`/%[1]s/{id}/detach/`) without releasing it. "+
			"Changing the server or address forces a new attachment.", r.family.name),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "`<server_id>/<address_id>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"server_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the server.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"address_id": schema.StringAttribute{
				Required: true, MarkdownDescription: fmt.Sprintf("ID of the `pidginhost_%s` address.", r.family.name),
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"reboot": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Restart the server after attaching so the guest OS picks up the address. " +
					"Only used when attaching. Defaults to `false`.",
			},
			"reboot_required": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the API reported that the guest needs a restart to use the address " +
					"and it was not restarted.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Delete: true}),
		},
	}
}

func (r *ipAttachmentResource) ids(m *ipAttachmentModel) (serverID, addressID int32, err error) {
	serverID, err = apiclient.ParseID(m.ServerID.ValueString())
	if err != nil {
		return 0, 0, err
	}
	addressID, err = apiclient.ParseID(m.AddressID.ValueString())
	return serverID, addressID, err
}

func (r *ipAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ipAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	serverID, addressID, err := r.ids(&plan)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	rebootRequired, httpResp, err := r.family.attach(ctx, r.Client, serverID, apiclient.FormatID(addressID), plan.Reboot.ValueBool())
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error attaching "+r.family.label, err, httpResp)
		return
	}
	plan.ID = types.StringValue(apiclient.JoinID(plan.ServerID.ValueString(), plan.AddressID.ValueString()))
	plan.RebootRequired = types.BoolValue(rebootRequired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if rebootRequired {
		resp.Diagnostics.AddWarning("Server restart required",
			fmt.Sprintf("%s %d was attached to server %d, but the guest OS needs a restart to use it. "+
				"Set reboot = true to restart automatically when attaching.", r.family.label, addressID, serverID))
	}
}

func (r *ipAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ipAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	serverID, addressID, err := r.ids(&state)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	ip, httpResp, err := r.family.get(ctx, r.Client, addressID)
	if apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+r.family.label, err, httpResp)
		return
	}
	if !ip.Attached || ip.ServerID == nil || *ip.ServerID != serverID {
		resp.State.RemoveResource(ctx)
		return
	}
	if state.Reboot.IsNull() {
		state.Reboot = types.BoolValue(false) // after import
	}
	if state.RebootRequired.IsNull() {
		state.RebootRequired = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only persists a changed reboot flag: it has no effect on an
// existing attachment.
func (r *ipAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ipAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ipAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ipAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	serverID, addressID, err := r.ids(&state)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	ip, httpResp, err := r.family.get(ctx, r.Client, addressID)
	if apiclient.IsNotFound(httpResp) {
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+r.family.label, err, httpResp)
		return
	}
	// Only detach if the address is still on this server: it may have been
	// moved elsewhere, and detaching it there would break another server.
	if !ip.Attached || ip.ServerID == nil || *ip.ServerID != serverID {
		return
	}
	if httpResp, err := r.family.detach(ctx, r.Client, addressID); err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error detaching "+r.family.label, err, httpResp)
	}
}

func (r *ipAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := apiclient.SplitID(req.ID, 2, "<server_id>/<address_id>")
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address_id"), parts[1])...)
}
