// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package volume

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.ResourceWithConfigure   = (*volumeResource)(nil)
	_ resource.ResourceWithImportState = (*volumeResource)(nil)
)

type volumeResource struct{ tfutil.ResourceBase }

// NewVolumeResource returns the pidginhost_volume resource.
func NewVolumeResource() resource.Resource { return &volumeResource{} }

type volumeModel struct {
	ID             types.String   `tfsdk:"id"`
	ServerID       types.String   `tfsdk:"server_id"`
	Product        types.String   `tfsdk:"product"`
	Size           types.Int64    `tfsdk:"size"`
	Alias          types.String   `tfsdk:"alias"`
	Project        types.String   `tfsdk:"project"`
	Attached       types.Bool     `tfsdk:"attached"`
	ServerHostname types.String   `tfsdk:"server_hostname"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *volumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A block storage volume, attached to a server.\n\n" +
			"The API can only create a volume on a server (`POST /api/cloud/servers/{id}/volumes/`), so `server_id` " +
			"is required. Changing `server_id` moves the volume in place (detach, then attach); the data is kept. " +
			"Growing `size` resizes the volume online; shrinking it is not possible and replaces the volume, " +
			"**destroying its data**.\n\n" +
			"Do not also manage the same volume with `pidginhost_volume_attachment`; that resource is for volumes " +
			"created outside this resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Volume ID.", PlanModifiers: stable},
			"server_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "ID of the server the volume is created on and attached to. Changing it moves the " +
					"volume to another server without losing data.",
				Validators: []validator.String{tfutil.IsID()},
			},
			"product": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Storage product, as a slug (see `pidginhost_storage_products`) or ID. " +
					"Changing it replaces the volume. The API's spelling of the product is not compared, so after " +
					"`terraform import` the configured value is adopted without a replacement.",
				PlanModifiers: []planmodifier.String{tfutil.CreateOnlyString()},
			},
			"size": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Size in GB. Increasing it resizes the volume online; decreasing it replaces the volume.",
				Validators:          []validator.Int64{int64validator.Between(1, 2147483647)},
				PlanModifiers:       []planmodifier.Int64{tfutil.RequiresReplaceIfDecreased()},
			},
			"alias": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Name of the volume, up to 50 characters. Can be changed in place.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 50)},
				PlanModifiers:       stable,
			},
			"project": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Project label. Can be changed in place.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(200)},
				PlanModifiers:       stable,
			},
			"attached": schema.BoolAttribute{
				Computed: true, MarkdownDescription: "Whether the volume is attached.",
			},
			"server_hostname": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Hostname of the server the volume is attached to.",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

// refresh reads the volume into m. gone is true when it no longer exists.
func (r *volumeResource) refresh(ctx context.Context, m *volumeModel) (gone bool, err error) {
	id, err := apiclient.ParseID(m.ID.ValueString())
	if err != nil {
		return false, err
	}
	v, httpResp, err := r.Client.Cloud().CloudVolumesRetrieve(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s", apiclient.ErrorDetail(err, httpResp))
	}
	var prior int32
	if tfutil.Known(m.ServerID) {
		prior, _ = apiclient.ParseID(m.ServerID.ValueString())
	}
	sid, err := attachedServer(ctx, r.Client, v, prior)
	if err != nil {
		return false, err
	}
	if sid != 0 {
		m.ServerID = types.StringValue(apiclient.FormatID(sid))
	} else {
		// Detached (or attached to a server that cannot be found): report
		// it so the next apply re-attaches the volume.
		m.ServerID = types.StringNull()
	}
	applyVolume(m, v)
	return false, nil
}

func applyVolume(m *volumeModel, v *sdk.Volume) {
	m.ID = types.StringValue(apiclient.FormatID(v.GetId()))
	// The product is returned as "ID or slug" without saying which, and it
	// cannot change after creation, so it is never read back: comparing the
	// API's spelling with the configuration could replace the volume and
	// destroy its data.
	m.Size = types.Int64Value(int64(v.GetSize()))
	m.Alias = types.StringValue(v.GetAlias())
	m.Project = types.StringValue(v.GetProject())
	m.Attached = types.BoolValue(v.GetAttached())
	m.ServerHostname = tfutil.StringOrNull(v.GetServer())
}

func (r *volumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewVolumeRequest(int32(plan.Size.ValueInt64()), plan.Product.ValueString())
	body.Alias = tfutil.StringPtr(plan.Alias)
	body.Project = tfutil.StringPtr(plan.Project)
	v, httpResp, err := r.Client.Cloud().CloudServersVolumesCreate(ctx, plan.ServerID.ValueString()).VolumeRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating volume", err, httpResp)
		return
	}
	plan.ID = types.StringValue(apiclient.FormatID(v.GetId()))
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), plan.ID)...)

	// Assumption: the spec answers 201 with the volume but does not say
	// whether it is attached at that point. Wait until it is.
	if err := waitAttached(ctx, r.Client, v.GetId(), true); err != nil {
		resp.Diagnostics.AddError("Volume did not attach", err.Error())
		return
	}
	if _, err := r.refresh(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading volume", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gone, err := r.refresh(ctx, &state)
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading volume", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *volumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state volumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}

	// Size (grow only; shrinking is planned as a replacement), alias and
	// project: PATCH /api/cloud/volumes/{id}/.
	patch := sdk.NewPatchedVolumeUpdateRequest()
	changed := false
	if !plan.Size.Equal(state.Size) {
		patch.SetSize(int32(plan.Size.ValueInt64()))
		changed = true
	}
	if tfutil.Known(plan.Alias) && !plan.Alias.Equal(state.Alias) {
		patch.SetAlias(plan.Alias.ValueString())
		changed = true
	}
	if tfutil.Known(plan.Project) && !plan.Project.Equal(state.Project) {
		patch.SetProject(plan.Project.ValueString())
		changed = true
	}
	if changed {
		if _, httpResp, err := r.Client.Cloud().CloudVolumesPartialUpdate(ctx, id).PatchedVolumeUpdateRequest(*patch).Execute(); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error updating volume", err, httpResp)
			return
		}
	}
	if !plan.Size.Equal(state.Size) {
		// Assumption: the spec does not say whether a resize completes
		// before PATCH returns. Wait until the API reports the new size.
		want := int32(plan.Size.ValueInt64())
		err := apiclient.WaitFor(ctx, r.Client.PollInterval, fmt.Sprintf("volume %d to reach %d GB", id, want),
			func(ctx context.Context) (bool, string, error) {
				v, httpResp, err := r.Client.Cloud().CloudVolumesRetrieve(ctx, id).Execute()
				if err != nil {
					return false, "", fmt.Errorf("%s", apiclient.ErrorDetail(err, httpResp))
				}
				return v.GetSize() == want, fmt.Sprintf("%d GB", v.GetSize()), nil
			})
		if err != nil {
			resp.Diagnostics.AddError("Volume resize did not complete", err.Error())
			return
		}
	}

	// Server: move the volume (detach from the old server, attach to the new).
	if !plan.ServerID.Equal(state.ServerID) {
		newServer, err := apiclient.ParseID(plan.ServerID.ValueString())
		if err != nil {
			tfutil.AddIDError(&resp.Diagnostics, err)
			return
		}
		v, httpResp, err := r.Client.Cloud().CloudVolumesRetrieve(ctx, id).Execute()
		if err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error reading volume", err, httpResp)
			return
		}
		if v.GetAttached() {
			if err := detach(ctx, r.Client, id); err != nil {
				resp.Diagnostics.AddError("Error moving volume", err.Error())
				return
			}
		}
		if err := attach(ctx, r.Client, id, newServer); err != nil {
			resp.Diagnostics.AddError("Error moving volume", err.Error())
			return
		}
	}

	gone, err := r.refresh(ctx, &plan)
	if gone {
		resp.Diagnostics.AddError("Volume disappeared", fmt.Sprintf("Volume %d was deleted while it was being updated.", id))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading volume", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	tfutil.ClearImported(ctx, resp)
}

func (r *volumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	v, httpResp, err := r.Client.Cloud().CloudVolumesRetrieve(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading volume", err, httpResp)
		return
	}
	// Assumption: the spec does not say whether an attached volume can be
	// deleted. Detach it first.
	if v.GetAttached() {
		if err := detach(ctx, r.Client, id); err != nil {
			resp.Diagnostics.AddError("Error detaching volume before deletion", err.Error())
			return
		}
	}
	if httpResp, err := r.Client.Cloud().CloudVolumesDestroy(ctx, id).Execute(); err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting volume", err, httpResp)
	}
}

func (r *volumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	tfutil.MarkImported(ctx, resp)
}
