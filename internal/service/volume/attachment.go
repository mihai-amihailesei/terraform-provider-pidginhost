// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package volume

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

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*attachmentResource)(nil)
	_ resource.ResourceWithImportState = (*attachmentResource)(nil)
)

type attachmentResource struct{ tfutil.ResourceBase }

// NewAttachmentResource returns the pidginhost_volume_attachment resource.
func NewAttachmentResource() resource.Resource { return &attachmentResource{} }

type attachmentModel struct {
	ID       types.String   `tfsdk:"id"`
	VolumeID types.String   `tfsdk:"volume_id"`
	ServerID types.String   `tfsdk:"server_id"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *attachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_attachment"
}

func (r *attachmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches an existing volume to a server (`/api/cloud/volumes/{id}/attach/`); destroying " +
			"it detaches the volume without deleting it.\n\n" +
			"Use it for volumes that are not managed by `pidginhost_volume`, for example the extra volume created " +
			"with a server (`extra_volume_product`) or volumes created in the control panel. A `pidginhost_volume` " +
			"already keeps its volume attached to its `server_id`; do not manage the same volume with both.\n\n" +
			"If the volume is already attached to the requested server, creating the attachment adopts it. If it is " +
			"attached to another server, creation fails rather than moving it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "`<volume_id>/<server_id>`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"volume_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the volume.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"server_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the server.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Delete: true}),
		},
	}
}

func (r *attachmentResource) ids(m *attachmentModel) (volumeID, serverID int32, err error) {
	if volumeID, err = apiclient.ParseID(m.VolumeID.ValueString()); err != nil {
		return
	}
	serverID, err = apiclient.ParseID(m.ServerID.ValueString())
	return
}

func (r *attachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan attachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	volumeID, serverID, err := r.ids(&plan)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	v, httpResp, err := r.Client.Cloud().CloudVolumesRetrieve(ctx, volumeID).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading volume", err, httpResp)
		return
	}
	if v.GetAttached() {
		has, _, err := serverHasVolume(ctx, r.Client, serverID, volumeID)
		if err != nil {
			resp.Diagnostics.AddError("Error reading server", err.Error())
			return
		}
		if !has {
			resp.Diagnostics.AddError("Volume is attached to another server",
				fmt.Sprintf("Volume %d is attached to %q. Detach it there first; this resource does not move volumes "+
					"between servers implicitly.", volumeID, v.GetServer()))
			return
		}
	} else if err := attach(ctx, r.Client, volumeID, serverID); err != nil {
		resp.Diagnostics.AddError("Error attaching volume", err.Error())
		return
	}
	plan.ID = types.StringValue(apiclient.JoinID(plan.VolumeID.ValueString(), plan.ServerID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *attachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	volumeID, serverID, err := r.ids(&state)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	has, _, err := serverHasVolume(ctx, r.Client, serverID, volumeID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading volume attachment", err.Error())
		return
	}
	if !has {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *attachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every argument forces replacement; there is nothing to update.
}

func (r *attachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state attachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	volumeID, serverID, err := r.ids(&state)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	// Only detach if the volume is still on this server.
	has, _, err := serverHasVolume(ctx, r.Client, serverID, volumeID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading volume attachment", err.Error())
		return
	}
	if !has {
		return
	}
	if err := detach(ctx, r.Client, volumeID); err != nil {
		resp.Diagnostics.AddError("Error detaching volume", err.Error())
	}
}

func (r *attachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := apiclient.SplitID(req.ID, 2, "<volume_id>/<server_id>")
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("volume_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[1])...)
}
