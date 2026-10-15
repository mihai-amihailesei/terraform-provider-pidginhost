// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package firewall implements firewall rule sets and their rules. A set is
// applied to a server through the server's public interface; see the
// firewall_rules_set_id attribute of pidginhost_server.
package firewall

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

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*rulesSetResource)(nil)
	_ resource.ResourceWithImportState = (*rulesSetResource)(nil)
)

type rulesSetResource struct{ tfutil.ResourceBase }

// NewRulesSetResource returns the pidginhost_firewall_rules_set resource.
func NewRulesSetResource() resource.Resource { return &rulesSetResource{} }

type rulesSetModel struct {
	ID       types.String   `tfsdk:"id"`
	Name     types.String   `tfsdk:"name"`
	Status   types.String   `tfsdk:"status"`
	ReadOnly types.Bool     `tfsdk:"read_only"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *rulesSetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_rules_set"
}

func (r *rulesSetResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A firewall rule set. Rules are managed with separate `pidginhost_firewall_rule` resources, " +
			"so adding, changing or removing a rule never recreates the set. Apply the set to a server with the " +
			"server's `firewall_rules_set_id` attribute.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Rule set ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the rule set, up to 200 characters. Can be changed in place.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 200)},
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Validation status of the set's rules: `validated`, `invalid` or `pending`. " +
					"It changes as rules are added, so it is not kept stable between plans.",
			},
			"read_only": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the set is read-only (used for free-tier servers).",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func applyRulesSet(m *rulesSetModel, s *sdk.FirewallRulesSet) {
	m.ID = types.StringValue(apiclient.FormatID(s.GetId()))
	m.Name = types.StringValue(s.GetName())
	m.Status = types.StringValue(string(s.GetStatus()))
	m.ReadOnly = types.BoolValue(s.GetReadOnly())
}

func (r *rulesSetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan rulesSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	set, httpResp, err := r.Client.Cloud().CloudFirewallRulesSetCreate(ctx).
		FirewallRulesSetRequest(*sdk.NewFirewallRulesSetRequest(plan.Name.ValueString())).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating firewall rule set", err, httpResp)
		return
	}
	applyRulesSet(&plan, set)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *rulesSetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state rulesSetModel
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
	set, httpResp, err := r.Client.Cloud().CloudFirewallRulesSetRetrieve(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading firewall rule set", err, httpResp)
		return
	}
	applyRulesSet(&state, set)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *rulesSetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan rulesSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(plan.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	body := sdk.NewPatchedFirewallRulesSetRequest()
	body.SetName(plan.Name.ValueString())
	set, httpResp, err := r.Client.Cloud().CloudFirewallRulesSetPartialUpdate(ctx, id).PatchedFirewallRulesSetRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error updating firewall rule set", err, httpResp)
		return
	}
	applyRulesSet(&plan, set)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *rulesSetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state rulesSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	httpResp, err := r.Client.Cloud().CloudFirewallRulesSetDestroy(ctx, id).Execute()
	if err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting firewall rule set", err, httpResp)
	}
}

func (r *rulesSetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
