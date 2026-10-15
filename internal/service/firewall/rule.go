// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package firewall

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*ruleResource)(nil)
	_ resource.ResourceWithImportState = (*ruleResource)(nil)
)

type ruleResource struct{ tfutil.ResourceBase }

// NewRuleResource returns the pidginhost_firewall_rule resource.
func NewRuleResource() resource.Resource { return &ruleResource{} }

type ruleModel struct {
	ID           types.String   `tfsdk:"id"`
	RulesSetID   types.String   `tfsdk:"rules_set_id"`
	Direction    types.String   `tfsdk:"direction"`
	Action       types.String   `tfsdk:"action"`
	Protocol     types.String   `tfsdk:"protocol"`
	Source       types.String   `tfsdk:"source"`
	SourcePort   types.String   `tfsdk:"source_port"`
	Destination  types.String   `tfsdk:"destination"`
	DestPort     types.String   `tfsdk:"destination_port"`
	Enabled      types.Bool     `tfsdk:"enabled"`
	Position     types.Int64    `tfsdk:"position"`
	HasError     types.Bool     `tfsdk:"has_error"`
	ErrorMessage types.String   `tfsdk:"error_message"`
	Timeouts     timeouts.Value `tfsdk:"timeouts"`
}

func (r *ruleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_rule"
}

func (r *ruleResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	addrDesc := "Single IP, range (`20.34.101.207-201.3.9.99`) or comma-separated list. Empty means any."
	portDesc := "Port number (0-65535), range (`80:85`) or comma-separated list. Empty means any. Requires `protocol`."
	textAttr := func(desc string, maxLen int) schema.StringAttribute {
		return schema.StringAttribute{
			Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			MarkdownDescription: desc,
			Validators:          []validator.String{stringvalidator.LengthAtMost(maxLen)},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A rule in a `pidginhost_firewall_rules_set`. Every field except `rules_set_id` can be changed in place.\n\n" +
			"The API validates rules after saving them. If it flags a rule as invalid (`has_error`), " +
			"apply fails with the API's `error_message`, so a broken firewall never goes unnoticed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Rule ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"rules_set_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the rule set. Changing it moves the rule by recreating it in the new set.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"direction": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`in` or `out`.",
				Validators:          []validator.String{stringvalidator.OneOf("in", "out")},
			},
			"action": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`ACCEPT`, `DROP` or `REJECT`.",
				Validators:          []validator.String{stringvalidator.OneOf("ACCEPT", "DROP", "REJECT")},
			},
			"protocol":         textAttr("Protocol, for example `tcp` or `udp`. Empty means any.", 100),
			"source":           textAttr("Source address. "+addrDesc, 500),
			"source_port":      textAttr("Source port. "+portDesc, 500),
			"destination":      textAttr("Destination address. "+addrDesc, 500),
			"destination_port": textAttr("Destination port. "+portDesc, 500),
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Whether the rule is active. Defaults to `true`.",
			},
			"position": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Position of the rule in the set (0-32767). When omitted, the API assigns one.",
				Validators:          []validator.Int64{int64validator.Between(0, 32767)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"has_error": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the API flagged the rule as invalid.",
			},
			"error_message": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The API's validation message when `has_error` is true.",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func applyRule(m *ruleModel, rule *sdk.FirewallRule) {
	m.ID = types.StringValue(apiclient.FormatID(rule.GetId()))
	m.Direction = types.StringValue(string(rule.GetDirection()))
	m.Action = types.StringValue(string(rule.GetAction()))
	m.Protocol = types.StringValue(rule.GetProtocol())
	m.Source = types.StringValue(rule.GetSource())
	m.SourcePort = types.StringValue(rule.GetSport())
	m.Destination = types.StringValue(rule.GetDestination())
	m.DestPort = types.StringValue(rule.GetDport())
	m.Enabled = types.BoolValue(rule.GetEnabled())
	m.Position = types.Int64Value(int64(rule.GetPosition()))
	m.HasError = types.BoolValue(rule.GetHasError())
	m.ErrorMessage = types.StringValue(rule.GetErrorMessage())
}

// checkRuleError fails the apply when the API flagged the rule as invalid.
// The state is saved first, so the rule is not orphaned.
//
// Assumption: the spec does not say whether validation (has_error) is
// computed synchronously when a rule is saved or later (the set has a
// "pending" status). This checks the value returned by the create/update
// call; a rule flagged later shows up as has_error=true on the next refresh.
func checkRuleError(m *ruleModel, add func(summary, detail string)) {
	if m.HasError.ValueBool() {
		add("Firewall rule is invalid", fmt.Sprintf("The API saved rule %s but flagged it as invalid: %s",
			m.ID.ValueString(), m.ErrorMessage.ValueString()))
	}
}

func (r *ruleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ruleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewFirewallRuleRequest(sdk.FirewallRuleDirectionEnum(plan.Direction.ValueString()), sdk.FwPolicyOutEnum(plan.Action.ValueString()))
	body.SetProtocol(plan.Protocol.ValueString())
	body.SetSource(plan.Source.ValueString())
	body.SetSport(plan.SourcePort.ValueString())
	body.SetDestination(plan.Destination.ValueString())
	body.SetDport(plan.DestPort.ValueString())
	body.SetEnabled(plan.Enabled.ValueBool())
	if tfutil.Known(plan.Position) {
		body.SetPosition(int32(plan.Position.ValueInt64()))
	}

	rule, httpResp, err := r.Client.Cloud().CloudFirewallRulesSetRulesCreate(ctx, plan.RulesSetID.ValueString()).FirewallRuleRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating firewall rule", err, httpResp)
		return
	}
	applyRule(&plan, rule)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	checkRuleError(&plan, resp.Diagnostics.AddError)
}

func (r *ruleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ruleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rule, httpResp, err := r.Client.Cloud().CloudFirewallRulesSetRulesRetrieve(ctx, state.ID.ValueString(), state.RulesSetID.ValueString()).Execute()
	if apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading firewall rule", err, httpResp)
		return
	}
	applyRule(&state, rule)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ruleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ruleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewPatchedFirewallRuleRequest()
	body.SetDirection(sdk.FirewallRuleDirectionEnum(plan.Direction.ValueString()))
	body.SetAction(sdk.FwPolicyOutEnum(plan.Action.ValueString()))
	body.SetProtocol(plan.Protocol.ValueString())
	body.SetSource(plan.Source.ValueString())
	body.SetSport(plan.SourcePort.ValueString())
	body.SetDestination(plan.Destination.ValueString())
	body.SetDport(plan.DestPort.ValueString())
	body.SetEnabled(plan.Enabled.ValueBool())
	if tfutil.Known(plan.Position) {
		body.SetPosition(int32(plan.Position.ValueInt64()))
	}

	rule, httpResp, err := r.Client.Cloud().CloudFirewallRulesSetRulesPartialUpdate(ctx, plan.ID.ValueString(), plan.RulesSetID.ValueString()).
		PatchedFirewallRuleRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error updating firewall rule", err, httpResp)
		return
	}
	applyRule(&plan, rule)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	checkRuleError(&plan, resp.Diagnostics.AddError)
}

func (r *ruleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ruleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpResp, err := r.Client.Cloud().CloudFirewallRulesSetRulesDestroy(ctx, state.ID.ValueString(), state.RulesSetID.ValueString()).Execute()
	if err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting firewall rule", err, httpResp)
	}
}

func (r *ruleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := apiclient.SplitID(req.ID, 2, "<rules_set_id>/<rule_id>")
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rules_set_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
