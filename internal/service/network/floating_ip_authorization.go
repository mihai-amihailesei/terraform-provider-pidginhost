// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
	_ resource.ResourceWithConfigure        = (*floatingAuthResource)(nil)
	_ resource.ResourceWithImportState      = (*floatingAuthResource)(nil)
	_ resource.ResourceWithConfigValidators = (*floatingAuthResource)(nil)
)

type floatingAuthResource struct{ tfutil.ResourceBase }

// NewFloatingIPAuthorizationResource returns the
// pidginhost_floating_ip_authorization resource.
func NewFloatingIPAuthorizationResource() resource.Resource { return &floatingAuthResource{} }

type floatingAuthModel struct {
	ID             types.String   `tfsdk:"id"`
	FloatingIPv4ID types.String   `tfsdk:"floating_ipv4_id"`
	FloatingIPv6ID types.String   `tfsdk:"floating_ipv6_id"`
	ServerID       types.String   `tfsdk:"server_id"`
	ServerHostname types.String   `tfsdk:"server_hostname"`
	CreatedAt      types.String   `tfsdk:"created_at"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *floatingAuthResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_floating_ip_authorization"
}

func (r *floatingAuthResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Authorizes a server to use a floating IP (`authorize` / `unauthorize`). " +
			"Set exactly one of `floating_ipv4_id` and `floating_ipv6_id`. Every argument forces a new authorization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "`<ipv4|ipv6>/<floating_ip_id>/<server_id>`.", PlanModifiers: stable,
			},
			"floating_ipv4_id": schema.StringAttribute{
				Optional: true, MarkdownDescription: "ID of a `pidginhost_floating_ipv4`.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"floating_ipv6_id": schema.StringAttribute{
				Optional: true, MarkdownDescription: "ID of a `pidginhost_floating_ipv6`.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"server_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the server to authorize.",
				Validators: []validator.String{tfutil.IsID()}, PlanModifiers: replace,
			},
			"server_hostname": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Hostname of the server, as reported by the API.", PlanModifiers: stable,
			},
			"created_at": schema.StringAttribute{
				Computed: true, MarkdownDescription: "When the authorization was created.", PlanModifiers: stable,
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Delete: true}),
		},
	}
}

func (r *floatingAuthResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("floating_ipv4_id"), path.MatchRoot("floating_ipv6_id")),
	}
}

func (r *floatingAuthResource) parse(m *floatingAuthModel) (fam floatingFamily, famName string, fipID, serverID int32, err error) {
	ref := m.FloatingIPv4ID
	fam, famName = floatingV4, "ipv4"
	if tfutil.Known(m.FloatingIPv6ID) {
		ref, fam, famName = m.FloatingIPv6ID, floatingV6, "ipv6"
	}
	if fipID, err = apiclient.ParseID(ref.ValueString()); err != nil {
		return
	}
	serverID, err = apiclient.ParseID(m.ServerID.ValueString())
	return
}

// find returns the authorization row for serverID, or nil.
func (r *floatingAuthResource) find(ctx context.Context, fam floatingFamily, fipID, serverID int32) (*sdk.FloatingIPAuthorization, bool, error) {
	all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.FloatingIPAuthorization, bool, *http.Response, error) {
		res, httpResp, err := fam.listAuth(ctx, r.Client, fipID, page)
		if err != nil {
			return nil, false, httpResp, err
		}
		return res.GetResults(), res.GetNext() != "", httpResp, nil
	})
	if err != nil {
		return nil, false, err
	}
	for _, a := range all {
		if a.GetServerId() == serverID {
			return &a, true, nil
		}
	}
	return nil, true, nil
}

func (r *floatingAuthResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan floatingAuthModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fam, famName, fipID, serverID, err := r.parse(&plan)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	if httpResp, err := fam.authorize(ctx, r.Client, fipID, serverID); err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error authorizing server for "+fam.label, err, httpResp)
		return
	}
	plan.ID = types.StringValue(apiclient.JoinID(famName, apiclient.FormatID(fipID), apiclient.FormatID(serverID)))
	a, _, err := r.find(ctx, fam, fipID, serverID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading floating IP authorizations", err.Error())
		return
	}
	if a == nil {
		resp.Diagnostics.AddError("Authorization not found",
			fmt.Sprintf("The API accepted the authorization of server %d for %s %d but does not list it.", serverID, fam.label, fipID))
		return
	}
	plan.ServerHostname = types.StringValue(a.GetServerHostname())
	plan.CreatedAt = types.StringValue(a.GetCreatedAt())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingAuthResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state floatingAuthModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fam, _, fipID, serverID, err := r.parse(&state)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	if _, httpResp, err := fam.get(ctx, r.Client, fipID); apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+fam.label, err, httpResp)
		return
	}
	a, _, err := r.find(ctx, fam, fipID, serverID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading floating IP authorizations", err.Error())
		return
	}
	if a == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ServerHostname = types.StringValue(a.GetServerHostname())
	state.CreatedAt = types.StringValue(a.GetCreatedAt())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *floatingAuthResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every argument forces replacement; there is nothing to update.
}

func (r *floatingAuthResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state floatingAuthModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fam, _, fipID, serverID, err := r.parse(&state)
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	if httpResp, err := fam.unauthorize(ctx, r.Client, fipID, serverID); err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error unauthorizing server for "+fam.label, err, httpResp)
	}
}

func (r *floatingAuthResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := apiclient.SplitID(req.ID, 3, "<ipv4|ipv6>/<floating_ip_id>/<server_id>")
	if err == nil && parts[0] != "ipv4" && parts[0] != "ipv6" {
		err = fmt.Errorf("unexpected ID %q: the first part must be ipv4 or ipv6", req.ID)
	}
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("floating_"+parts[0]+"_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), parts[2])...)
}
