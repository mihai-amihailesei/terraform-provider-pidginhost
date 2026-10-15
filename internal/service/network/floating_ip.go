// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

// floatingInfo is the part of FloatingIPv4/FloatingIPv6 the provider uses.
type floatingInfo struct {
	ID                int32
	Address           string
	Gateway           string
	Prefix            int32
	ReverseDNS        string
	Label             string
	AuthorizedVMCount int32
	CreatedAt         string
}

// floatingFamily binds the floating IPv4 or IPv6 endpoints of the SDK.
type floatingFamily struct {
	name, label string
	create      func(ctx context.Context, c *apiclient.Client, label string) (*floatingInfo, *http.Response, error)
	get         func(ctx context.Context, c *apiclient.Client, id int32) (*floatingInfo, *http.Response, error)
	destroy     func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error)
	setRDNS     func(ctx context.Context, c *apiclient.Client, id int32, name string) (*http.Response, error)
	authorize   func(ctx context.Context, c *apiclient.Client, id, serverID int32) (*http.Response, error)
	unauthorize func(ctx context.Context, c *apiclient.Client, id, serverID int32) (*http.Response, error)
	listAuth    func(ctx context.Context, c *apiclient.Client, id, page int32) (*sdk.PaginatedFloatingIPAuthorizationList, *http.Response, error)
}

var floatingV4 = floatingFamily{
	name: "ipv4", label: "floating IPv4",
	create: func(ctx context.Context, c *apiclient.Client, label string) (*floatingInfo, *http.Response, error) {
		body := sdk.NewFloatingIPv4CreateRequest()
		body.SetLabel(label)
		ip, r, err := c.Cloud().CloudFloatingIpv4Create(ctx).FloatingIPv4CreateRequest(*body).Execute()
		return fromFloatingV4(ip), r, err
	},
	get: func(ctx context.Context, c *apiclient.Client, id int32) (*floatingInfo, *http.Response, error) {
		ip, r, err := c.Cloud().CloudFloatingIpv4Retrieve(ctx, id).Execute()
		return fromFloatingV4(ip), r, err
	},
	destroy: func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error) {
		return c.Cloud().CloudFloatingIpv4Destroy(ctx, id).Execute()
	},
	setRDNS: func(ctx context.Context, c *apiclient.Client, id int32, name string) (*http.Response, error) {
		_, r, err := c.Cloud().CloudFloatingIpv4RdnsCreate(ctx, id).ReverseDNSRequest(*sdk.NewReverseDNSRequest(name)).Execute()
		return r, err
	},
	authorize: func(ctx context.Context, c *apiclient.Client, id, serverID int32) (*http.Response, error) {
		_, r, err := c.Cloud().CloudFloatingIpv4AuthorizeCreate(ctx, id).FloatingIPAuthorizeRequest(*sdk.NewFloatingIPAuthorizeRequest(serverID)).Execute()
		return r, err
	},
	unauthorize: func(ctx context.Context, c *apiclient.Client, id, serverID int32) (*http.Response, error) {
		_, r, err := c.Cloud().CloudFloatingIpv4UnauthorizeCreate(ctx, id).FloatingIPAuthorizeRequest(*sdk.NewFloatingIPAuthorizeRequest(serverID)).Execute()
		return r, err
	},
	listAuth: func(ctx context.Context, c *apiclient.Client, id, page int32) (*sdk.PaginatedFloatingIPAuthorizationList, *http.Response, error) {
		return c.Cloud().CloudFloatingIpv4AuthorizationsList(ctx, id).Page(page).Execute()
	},
}

var floatingV6 = floatingFamily{
	name: "ipv6", label: "floating IPv6",
	create: func(ctx context.Context, c *apiclient.Client, label string) (*floatingInfo, *http.Response, error) {
		body := sdk.NewFloatingIPv6CreateRequest()
		body.SetLabel(label)
		ip, r, err := c.Cloud().CloudFloatingIpv6Create(ctx).FloatingIPv6CreateRequest(*body).Execute()
		return fromFloatingV6(ip), r, err
	},
	get: func(ctx context.Context, c *apiclient.Client, id int32) (*floatingInfo, *http.Response, error) {
		ip, r, err := c.Cloud().CloudFloatingIpv6Retrieve(ctx, id).Execute()
		return fromFloatingV6(ip), r, err
	},
	destroy: func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error) {
		return c.Cloud().CloudFloatingIpv6Destroy(ctx, id).Execute()
	},
	setRDNS: func(ctx context.Context, c *apiclient.Client, id int32, name string) (*http.Response, error) {
		_, r, err := c.Cloud().CloudFloatingIpv6RdnsCreate(ctx, id).ReverseDNSRequest(*sdk.NewReverseDNSRequest(name)).Execute()
		return r, err
	},
	authorize: func(ctx context.Context, c *apiclient.Client, id, serverID int32) (*http.Response, error) {
		_, r, err := c.Cloud().CloudFloatingIpv6AuthorizeCreate(ctx, id).FloatingIPAuthorizeRequest(*sdk.NewFloatingIPAuthorizeRequest(serverID)).Execute()
		return r, err
	},
	unauthorize: func(ctx context.Context, c *apiclient.Client, id, serverID int32) (*http.Response, error) {
		_, r, err := c.Cloud().CloudFloatingIpv6UnauthorizeCreate(ctx, id).FloatingIPAuthorizeRequest(*sdk.NewFloatingIPAuthorizeRequest(serverID)).Execute()
		return r, err
	},
	listAuth: func(ctx context.Context, c *apiclient.Client, id, page int32) (*sdk.PaginatedFloatingIPAuthorizationList, *http.Response, error) {
		return c.Cloud().CloudFloatingIpv6AuthorizationsList(ctx, id).Page(page).Execute()
	},
}

func fromFloatingV4(ip *sdk.FloatingIPv4) *floatingInfo {
	if ip == nil {
		return nil
	}
	return &floatingInfo{ID: ip.GetId(), Address: ip.GetAddress(), Gateway: ip.GetGateway(), Prefix: ip.GetPrefix(),
		ReverseDNS: ip.GetReverseDns(), Label: ip.GetLabel(), AuthorizedVMCount: ip.GetAuthorizedVmCount(), CreatedAt: ip.GetCreatedAt()}
}

func fromFloatingV6(ip *sdk.FloatingIPv6) *floatingInfo {
	if ip == nil {
		return nil
	}
	return &floatingInfo{ID: ip.GetId(), Address: ip.GetAddress(), Gateway: ip.GetGateway(), Prefix: ip.GetPrefix(),
		ReverseDNS: ip.GetReverseDns(), Label: ip.GetLabel(), AuthorizedVMCount: ip.GetAuthorizedVmCount(), CreatedAt: ip.GetCreatedAt()}
}

var (
	_ resource.ResourceWithConfigure   = (*floatingIPResource)(nil)
	_ resource.ResourceWithImportState = (*floatingIPResource)(nil)
)

type floatingIPResource struct {
	tfutil.ResourceBase
	family floatingFamily
}

// NewFloatingIPv4Resource returns the pidginhost_floating_ipv4 resource.
func NewFloatingIPv4Resource() resource.Resource { return &floatingIPResource{family: floatingV4} }

// NewFloatingIPv6Resource returns the pidginhost_floating_ipv6 resource.
func NewFloatingIPv6Resource() resource.Resource { return &floatingIPResource{family: floatingV6} }

type floatingIPModel struct {
	ID                types.String   `tfsdk:"id"`
	Label             types.String   `tfsdk:"label"`
	ReverseDNS        types.String   `tfsdk:"reverse_dns"`
	Address           types.String   `tfsdk:"address"`
	Gateway           types.String   `tfsdk:"gateway"`
	Prefix            types.Int64    `tfsdk:"prefix"`
	AuthorizedVMCount types.Int64    `tfsdk:"authorized_server_count"`
	CreatedAt         types.String   `tfsdk:"created_at"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func (r *floatingIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_floating_" + r.family.name
}

func (r *floatingIPResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf("A %s address that can move between servers. Allow a server to use it with "+
			"`pidginhost_floating_ip_authorization`.", r.family.label),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Floating IP ID.", PlanModifiers: stable},
			"label": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Label, up to 64 characters. **The API cannot change the label of an existing floating " +
					"IP, so changing it replaces the floating IP and its address changes.**",
				Validators:    []validator.String{stringvalidator.LengthAtMost(64)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"reverse_dns": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Reverse DNS (PTR) name. Can be changed in place. When omitted, the API's default is kept.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 200)},
				PlanModifiers:       stable,
			},
			"address": schema.StringAttribute{Computed: true, MarkdownDescription: "The floating address.", PlanModifiers: stable},
			"gateway": schema.StringAttribute{Computed: true, MarkdownDescription: "Gateway for the address.", PlanModifiers: stable},
			"prefix": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Prefix length.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"authorized_server_count": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Number of servers authorized to use the address.",
			},
			"created_at": schema.StringAttribute{Computed: true, MarkdownDescription: "Creation time.", PlanModifiers: stable},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func applyFloating(m *floatingIPModel, ip *floatingInfo) {
	m.ID = types.StringValue(apiclient.FormatID(ip.ID))
	m.Label = types.StringValue(ip.Label)
	if !tfutil.Known(m.ReverseDNS) || !sameRDNS(m.ReverseDNS.ValueString(), ip.ReverseDNS) {
		m.ReverseDNS = types.StringValue(ip.ReverseDNS)
	}
	m.Address = types.StringValue(ip.Address)
	m.Gateway = types.StringValue(ip.Gateway)
	m.Prefix = types.Int64Value(int64(ip.Prefix))
	m.AuthorizedVMCount = types.Int64Value(int64(ip.AuthorizedVMCount))
	m.CreatedAt = types.StringValue(ip.CreatedAt)
}

func (r *floatingIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan floatingIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ip, httpResp, err := r.family.create(ctx, r.Client, plan.Label.ValueString())
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating "+r.family.label, err, httpResp)
		return
	}
	if tfutil.Known(plan.ReverseDNS) && !sameRDNS(plan.ReverseDNS.ValueString(), ip.ReverseDNS) {
		if httpResp, err := r.family.setRDNS(ctx, r.Client, ip.ID, plan.ReverseDNS.ValueString()); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error setting reverse DNS", err, httpResp)
			plan.ReverseDNS = types.StringNull()
			applyFloating(&plan, ip)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		}
		if ip, httpResp, err = r.family.get(ctx, r.Client, ip.ID); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+r.family.label, err, httpResp)
			return
		}
	}
	applyFloating(&plan, ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state floatingIPModel
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
	ip, httpResp, err := r.family.get(ctx, r.Client, id)
	if apiclient.IsNotFound(httpResp) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+r.family.label, err, httpResp)
		return
	}
	applyFloating(&state, ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *floatingIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan floatingIPModel
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
	if tfutil.Known(plan.ReverseDNS) {
		if httpResp, err := r.family.setRDNS(ctx, r.Client, id, plan.ReverseDNS.ValueString()); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error setting reverse DNS", err, httpResp)
			return
		}
	}
	ip, httpResp, err := r.family.get(ctx, r.Client, id)
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading "+r.family.label, err, httpResp)
		return
	}
	applyFloating(&plan, ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state floatingIPModel
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
	if httpResp, err := r.family.destroy(ctx, r.Client, id); err != nil && !apiclient.IsNotFound(httpResp) {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting "+r.family.label, err, httpResp)
	}
}

func (r *floatingIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
