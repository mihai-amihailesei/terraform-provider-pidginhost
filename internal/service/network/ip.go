// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*ipResource)(nil)
	_ resource.ResourceWithImportState = (*ipResource)(nil)
)

type ipResource struct {
	tfutil.ResourceBase
	family ipFamily
}

// NewIPv4Resource returns the pidginhost_ipv4 resource.
func NewIPv4Resource() resource.Resource { return &ipResource{family: ipv4Family} }

// NewIPv6Resource returns the pidginhost_ipv6 resource.
func NewIPv6Resource() resource.Resource { return &ipResource{family: ipv6Family} }

type ipModel struct {
	ID         types.String   `tfsdk:"id"`
	Address    types.String   `tfsdk:"address"`
	Gateway    types.String   `tfsdk:"gateway"`
	Prefix     types.Int64    `tfsdk:"prefix"`
	Slug       types.String   `tfsdk:"slug"`
	ReverseDNS types.String   `tfsdk:"reverse_dns"`
	Attached   types.Bool     `tfsdk:"attached"`
	ServerID   types.String   `tfsdk:"server_id"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

func (r *ipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.family.name
}

func (r *ipResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf("Allocates a public %[1]s address. Attach it to a server with "+
			"`pidginhost_%[2]s_attachment`, or pass it to a new server as `public_%[2]s`.", r.family.label, r.family.name),
		Attributes: map[string]schema.Attribute{
			"id":      schema.StringAttribute{Computed: true, MarkdownDescription: "Address ID.", PlanModifiers: stable},
			"address": schema.StringAttribute{Computed: true, MarkdownDescription: "The allocated address.", PlanModifiers: stable},
			"gateway": schema.StringAttribute{Computed: true, MarkdownDescription: "Gateway for the address.", PlanModifiers: stable},
			"prefix": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Prefix length of the address's network.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{Computed: true, MarkdownDescription: "Slug of the address.", PlanModifiers: stable},
			"reverse_dns": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Reverse DNS (PTR) name, for example `host.example.com`. Can be changed in place. " +
					"When omitted, the API's default is kept. The API cannot remove a PTR record, so removing this " +
					"argument leaves the current name in place.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 200)},
				PlanModifiers: stable,
			},
			"attached": schema.BoolAttribute{
				Computed: true, MarkdownDescription: "Whether the address is attached to a server.",
			},
			"server_id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "ID of the server the address is attached to, or null.",
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func applyIP(m *ipModel, ip *ipInfo) {
	m.ID = types.StringValue(apiclient.FormatID(ip.ID))
	m.Address = types.StringValue(ip.Address)
	m.Gateway = types.StringValue(ip.Gateway)
	m.Prefix = types.Int64Value(int64(ip.Prefix))
	m.Slug = types.StringValue(ip.Slug)
	m.Attached = types.BoolValue(ip.Attached)
	if ip.ServerID != nil {
		m.ServerID = types.StringValue(apiclient.FormatID(*ip.ServerID))
	} else {
		m.ServerID = types.StringNull()
	}
}

// sameRDNS compares PTR names, ignoring case and a trailing dot.
func sameRDNS(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

func (r *ipResource) read(ctx context.Context, m *ipModel) (gone bool, err error) {
	id, err := apiclient.ParseID(m.ID.ValueString())
	if err != nil {
		return false, err
	}
	ip, httpResp, err := r.family.get(ctx, r.Client, id)
	if apiclient.IsNotFound(httpResp) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s", apiclient.ErrorDetail(err, httpResp))
	}
	applyIP(m, ip)
	name, httpResp, err := r.family.getRDNS(ctx, r.Client, id)
	if err != nil {
		return false, fmt.Errorf("reading reverse DNS: %s", apiclient.ErrorDetail(err, httpResp))
	}
	if !tfutil.Known(m.ReverseDNS) || !sameRDNS(m.ReverseDNS.ValueString(), name) {
		m.ReverseDNS = types.StringValue(name)
	}
	return false, nil
}

func (r *ipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ip, httpResp, err := r.family.create(ctx, r.Client)
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error allocating "+r.family.label, err, httpResp)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), apiclient.FormatID(ip.ID))...)

	if tfutil.Known(plan.ReverseDNS) {
		if httpResp, err := r.family.setRDNS(ctx, r.Client, ip.ID, plan.ReverseDNS.ValueString()); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error setting reverse DNS", err, httpResp)
			// The address exists: save it so it is tracked (as tainted).
			applyIP(&plan, ip)
			plan.ReverseDNS = types.StringNull()
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		}
	}

	plan.ID = types.StringValue(apiclient.FormatID(ip.ID))
	if _, err := r.read(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading "+r.family.label, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gone, err := r.read(ctx, &state)
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.family.label, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	if tfutil.Known(plan.ReverseDNS) && !sameRDNS(plan.ReverseDNS.ValueString(), state.ReverseDNS.ValueString()) {
		if httpResp, err := r.family.setRDNS(ctx, r.Client, id, plan.ReverseDNS.ValueString()); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error setting reverse DNS", err, httpResp)
			return
		}
	}
	if _, err := r.read(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error reading "+r.family.label, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ipModel
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

	// Assumption: the spec does not say whether an attached address can be
	// deleted. If it is still attached (for example attached outside
	// Terraform), detach it first so the delete does not fail.
	ip, httpResp, err := r.family.get(ctx, r.Client, id)
	if apiclient.IsNotFound(httpResp) {
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+r.family.label, err, httpResp)
		return
	}
	if ip.Attached {
		if httpResp, err := r.family.detach(ctx, r.Client, id); err != nil && !apiclient.IsNotFound(httpResp) {
			tfutil.AddAPIError(&resp.Diagnostics, "Error detaching "+r.family.label+" before deletion", err, httpResp)
			return
		}
	}
	if httpResp, err := r.family.destroy(ctx, r.Client, id); err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error releasing "+r.family.label, err, httpResp)
	}
}

func (r *ipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
