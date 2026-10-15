// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package account implements account-level resources: SSH keys.
package account

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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
	_ resource.ResourceWithConfigure   = (*sshKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshKeyResource)(nil)
)

type sshKeyResource struct{ tfutil.ResourceBase }

// NewSSHKeyResource returns the pidginhost_ssh_key resource.
func NewSSHKeyResource() resource.Resource { return &sshKeyResource{} }

type sshKeyModel struct {
	ID          types.String   `tfsdk:"id"`
	Alias       types.String   `tfsdk:"alias"`
	PublicKey   types.String   `tfsdk:"public_key"`
	Fingerprint types.String   `tfsdk:"fingerprint"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An SSH public key stored in the account (`/api/account/ssh-keys/`). " +
			"Reference it from a server with `ssh_key_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "SSH key ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"alias": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Name of the key, up to 50 characters. Can be changed in place.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 50)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"public_key": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The public key in OpenSSH format. Changing it replaces the resource, " +
					"because the API cannot update a stored key. Surrounding whitespace is ignored when comparing with the API.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 3000)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fingerprint": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fingerprint of the key, as computed by the API.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func (r *sshKeyResource) apply(m *sshKeyModel, k *sdk.SSHKey) {
	m.ID = types.StringValue(apiclient.FormatID(k.GetId()))
	m.Alias = types.StringValue(k.GetAlias())
	m.PublicKey = tfutil.KeepIfSameText(m.PublicKey, k.GetKey())
	m.Fingerprint = types.StringValue(k.GetFingerprint())
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewSSHKeyRequest(plan.PublicKey.ValueString())
	if tfutil.Known(plan.Alias) {
		body.SetAlias(plan.Alias.ValueString())
	}
	key, httpResp, err := r.Client.Account().AccountSshKeysCreate(ctx).SSHKeyRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating SSH key", err, httpResp)
		return
	}
	r.apply(&plan, key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	key, httpResp, err := r.Client.Account().AccountSshKeysRetrieve(ctx, state.ID.ValueString()).Execute()
	if apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading SSH key", err, httpResp)
		return
	}
	r.apply(&state, key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewPatchedSSHKeyUpdateRequest()
	body.SetAlias(plan.Alias.ValueString())
	key, httpResp, err := r.Client.Account().AccountSshKeysPartialUpdate(ctx, state.ID.ValueString()).PatchedSSHKeyUpdateRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error updating SSH key", err, httpResp)
		return
	}
	r.apply(&plan, key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpResp, err := r.Client.Account().AccountSshKeysDestroy(ctx, state.ID.ValueString()).Execute()
	if err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting SSH key", err, httpResp)
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
