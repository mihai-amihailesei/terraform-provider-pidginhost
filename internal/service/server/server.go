// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package server implements the pidginhost_server resource and data source.
package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*serverResource)(nil)
	_ resource.ResourceWithImportState = (*serverResource)(nil)
)

const (
	defaultCreateTimeout = 30 * time.Minute
	defaultUpdateTimeout = 30 * time.Minute
	defaultDeleteTimeout = 20 * time.Minute
	defaultReadTimeout   = 5 * time.Minute
	shutdownGracePeriod  = 3 * time.Minute
)

type serverResource struct{ tfutil.ResourceBase }

// NewServerResource returns the pidginhost_server resource.
func NewServerResource() resource.Resource { return &serverResource{} }

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = serverSchema(ctx)
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, defaultCreateTimeout)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := sdk.NewServerAddRequest(plan.Image.ValueString(), plan.Package.ValueString())
	body.Hostname = tfutil.StringPtr(plan.Hostname)
	body.Project = tfutil.StringPtr(plan.Project)
	body.Password = tfutil.StringPtr(plan.Password)
	body.SshPubKey = tfutil.StringPtr(plan.SSHPublicKey)
	body.SshPubKeyId = tfutil.StringPtr(plan.SSHKeyID)
	body.UserData = tfutil.StringPtr(plan.UserData)
	body.PublicIp = tfutil.StringPtr(plan.PublicIPv4)
	body.NewIpv4 = tfutil.BoolPtr(plan.NewIPv4)
	body.PublicIpv6 = tfutil.StringPtr(plan.PublicIPv6)
	body.NewIpv6 = tfutil.BoolPtr(plan.NewIPv6)
	body.NoNetworkAcknowledged = tfutil.BoolPtr(plan.NoNetworkAcknowledged)
	body.PrivateNetwork = tfutil.StringPtr(plan.PrivateNetworkID)
	body.PrivateAddress = tfutil.StringPtr(plan.PrivateAddress)
	body.ExtraVolumeProduct = tfutil.StringPtr(plan.ExtraVolumeProduct)
	body.ExtraVolumeSize = tfutil.Int32Ptr(plan.ExtraVolumeSize)
	body.EnableHa = tfutil.BoolPtr(plan.EnableHA)
	body.Generation = tfutil.StringPtr(plan.Generation)
	body.FwRulesSet = tfutil.StringPtr(plan.FirewallRulesSetID)
	if tfutil.Known(plan.FirewallPolicyIn) {
		body.SetFwPolicyIn(sdk.FwPolicyOutEnum(plan.FirewallPolicyIn.ValueString()))
	}
	if tfutil.Known(plan.FirewallPolicyOut) {
		body.SetFwPolicyOut(sdk.FwPolicyOutEnum(plan.FirewallPolicyOut.ValueString()))
	}

	created, httpResp, err := r.Client.Cloud().CloudServersCreate(ctx).ServerAddRequest(*body).Execute()
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating server", err, httpResp)
		return
	}
	id := created.GetId()

	// Record the ID before waiting. If provisioning fails or times out, the
	// resource is saved as tainted and replaced on the next apply, instead
	// of a billable server being left outside Terraform's control.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), apiclient.FormatID(id))...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, "server created, waiting for it to become active", map[string]any{"server_id": id})

	if _, err := waitForActive(ctx, r.Client, id, "creation"); err != nil {
		resp.Diagnostics.AddError("Server did not become active", err.Error())
		r.saveAfterFailure(ctx, &plan, id, &resp.State, &resp.Diagnostics)
		return
	}

	if plan.DestroyProtection.ValueBool() {
		if err := r.setDestroyProtection(ctx, id, true); err != nil {
			resp.Diagnostics.AddError("Error enabling destroy protection", err.Error())
			r.saveAfterFailure(ctx, &plan, id, &resp.State, &resp.Diagnostics)
			return
		}
	}
	if plan.PowerState.ValueString() == powerStopped {
		if err := setPower(ctx, r.Client, id, powerStopped, shutdownGracePeriod); err != nil {
			resp.Diagnostics.AddError("Error stopping server", err.Error())
			r.saveAfterFailure(ctx, &plan, id, &resp.State, &resp.Diagnostics)
			return
		}
	}

	plan.ID = types.StringValue(apiclient.FormatID(id))
	if _, d := r.refresh(ctx, &plan, true); d.HasError() {
		resp.Diagnostics.Append(d...)
		r.saveAfterFailure(ctx, &plan, id, &resp.State, &resp.Diagnostics)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// saveAfterFailure stores what is known about a server whose creation did
// not complete, so that Terraform tracks (and taints) it. Unknown computed
// values must not be saved, so they are nulled.
func (r *serverResource) saveAfterFailure(ctx context.Context, plan *serverModel, id int32, state interface {
	Set(context.Context, interface{}) diag.Diagnostics
}, diags *diag.Diagnostics) {
	plan.ID = types.StringValue(apiclient.FormatID(id))
	_, _ = r.refresh(ctx, plan, true)
	nullUnknowns(plan)
	diags.Append(state.Set(ctx, plan)...)
}

func nullUnknowns(m *serverModel) {
	for _, s := range []*types.String{&m.Hostname, &m.Generation, &m.Project, &m.SSHPublicKey, &m.FirewallPolicyIn,
		&m.FirewallPolicyOut, &m.PowerState, &m.Status, &m.ImageName, &m.Username, &m.IPv4Address, &m.IPv6Address} {
		if s.IsUnknown() {
			*s = types.StringNull()
		}
	}
	for _, b := range []*types.Bool{&m.EnableHA, &m.DestroyProtection} {
		if b.IsUnknown() {
			*b = types.BoolNull()
		}
	}
	for _, i := range []*types.Int64{&m.CPUs, &m.Memory, &m.DiskSize} {
		if i.IsUnknown() {
			*i = types.Int64Null()
		}
	}
	if m.PrivateNetworks.IsUnknown() {
		m.PrivateNetworks = types.ListNull(privateNetworkObjectType)
	}
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, defaultReadTimeout)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gone, d := r.refresh(ctx, &state, false)
	resp.Diagnostics.Append(d...)
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// refresh reads the server, its public interface and its power state into m.
// gone is true when the server no longer exists. During create, the package
// is taken as planned; otherwise it is compared with the API's value.
func (r *serverResource) refresh(ctx context.Context, m *serverModel, creating bool) (gone bool, diags diag.Diagnostics) {
	id, err := apiclient.ParseID(m.ID.ValueString())
	if err != nil {
		diags.AddError("Invalid ID", err.Error())
		return false, diags
	}
	s, httpResp, err := r.Client.Cloud().CloudServersRetrieve(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return true, diags
	}
	if err != nil {
		diags.AddError("Error reading server", apiclient.ErrorDetail(err, httpResp))
		return false, diags
	}
	if apiclient.IsGoneStatus(string(s.GetStatus())) {
		return true, diags
	}

	applyDetail(ctx, m, s, &diags)

	if !creating && tfutil.Known(m.Package) {
		same, err := samePackage(ctx, r.Client, m.Package.ValueString(), s.GetPackage())
		if err != nil {
			diags.AddError("Error comparing server packages", err.Error())
			return false, diags
		}
		if !same {
			m.Package = types.StringValue(s.GetPackage())
		}
	}

	pi, found, err := publicInterface(ctx, r.Client, id)
	if err != nil {
		diags.AddError("Error reading server firewall", err.Error())
		return false, diags
	}
	if found {
		setID, err := resolveRulesSetID(ctx, r.Client, pi.GetFwRulesSet())
		if err != nil {
			diags.AddError("Error reading server firewall", err.Error())
			return false, diags
		}
		m.FirewallRulesSetID = tfutil.StringOrNull(setID)
		m.FirewallPolicyIn = tfutil.StringOrNull(string(pi.GetFwPolicyIn()))
		m.FirewallPolicyOut = tfutil.StringOrNull(string(pi.GetFwPolicyOut()))
	} else {
		m.FirewallRulesSetID = types.StringNull()
		m.FirewallPolicyIn = types.StringNull()
		m.FirewallPolicyOut = types.StringNull()
	}

	power, err := powerState(ctx, r.Client, id, s)
	if err != nil {
		// The power state can be unavailable while a server is busy (for
		// example during a package change). Keep the previous value rather
		// than failing the whole refresh.
		tflog.Warn(ctx, "could not read power state", map[string]any{"server_id": id, "error": err.Error()})
		if m.PowerState.IsUnknown() {
			m.PowerState = types.StringNull()
		}
	} else {
		m.PowerState = types.StringValue(power)
	}
	return false, diags
}

// applyDetail copies the API's view of a server into m.
func applyDetail(ctx context.Context, m *serverModel, s *sdk.ServerDetail, diags *diag.Diagnostics) {
	m.ID = types.StringValue(apiclient.FormatID(s.GetId()))

	// Hostname and generation cannot be changed through the API, so a
	// different spelling of the same value (case, or the FQDN form of a
	// short hostname) must not be reported as drift: it would force a
	// replacement.
	if !tfutil.Known(m.Hostname) || !sameHostname(m.Hostname.ValueString(), s.GetHostname()) {
		m.Hostname = types.StringValue(s.GetHostname())
	}
	if !tfutil.Known(m.Generation) {
		m.Generation = tfutil.StringOrNull(s.GetGeneration())
	}

	m.Project = types.StringValue(s.GetProject())
	m.EnableHA = types.BoolValue(s.GetHaEnabled())
	m.SSHPublicKey = tfutil.KeepIfSameText(m.SSHPublicKey, s.GetSshPubKey())
	m.DestroyProtection = types.BoolValue(s.GetDestroyProtection())
	m.Status = types.StringValue(string(s.GetStatus()))
	m.ImageName = types.StringValue(s.GetImage())
	m.CPUs = types.Int64Value(int64(s.GetCpus()))
	m.Memory = types.Int64Value(int64(s.GetMemory()))
	m.DiskSize = types.Int64Value(int64(s.GetDiskSize()))
	m.Username = types.StringValue(s.GetUsername())
	if !tfutil.Known(m.Package) {
		m.Package = types.StringValue(s.GetPackage())
	}

	nets := s.GetNetworks()
	pub := nets.GetPublic()
	m.IPv4Address = tfutil.StringOrNull(pub.GetIpv4())
	m.IPv6Address = tfutil.StringOrNull(pub.GetIpv6())

	type privNet struct {
		Network   string `tfsdk:"network"`
		Address   string `tfsdk:"address"`
		Interface string `tfsdk:"interface"`
	}
	privs := make([]privNet, 0, len(nets.GetPrivate()))
	for _, p := range nets.GetPrivate() {
		privs = append(privs, privNet{Network: p.GetNetwork(), Address: p.GetAddress(), Interface: p.GetInterface()})
	}
	list, d := types.ListValueFrom(ctx, privateNetworkObjectType, privs)
	diags.Append(d...)
	m.PrivateNetworks = list
}

func sameHostname(configured, api string) bool {
	return strings.EqualFold(configured, api) ||
		strings.HasPrefix(strings.ToLower(api), strings.ToLower(configured)+".")
}

func (r *serverResource) setDestroyProtection(ctx context.Context, id int32, enabled bool) error {
	_, httpResp, err := r.Client.Cloud().CloudServersDestroyProtectionCreate(ctx, id).
		DestroyProtectionRequest(*sdk.NewDestroyProtectionRequest(enabled)).Execute()
	if err != nil {
		return fmt.Errorf("%s", apiclient.ErrorDetail(err, httpResp))
	}
	return nil
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, defaultUpdateTimeout)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}

	// Project, password and SSH key: PATCH /api/cloud/servers/{id}/.
	patch := sdk.NewPatchedServerDetailRequest()
	changed := false
	if tfutil.Known(plan.Project) && !plan.Project.Equal(state.Project) {
		patch.SetProject(plan.Project.ValueString())
		changed = true
	}
	if tfutil.Known(plan.Password) && !plan.Password.Equal(state.Password) {
		patch.SetPassword(plan.Password.ValueString())
		changed = true
	}
	if tfutil.Known(plan.SSHPublicKey) && !tfutil.SameText(plan.SSHPublicKey.ValueString(), state.SSHPublicKey.ValueString()) {
		patch.SetSshPubKey(plan.SSHPublicKey.ValueString())
		changed = true
	}
	if changed {
		if _, httpResp, err := r.Client.Cloud().CloudServersPartialUpdate(ctx, id).PatchedServerDetailRequest(*patch).Execute(); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error updating server", err, httpResp)
			return
		}
	}

	// Package: POST modify-package, then wait for the resize to finish.
	//
	// Assumption: the spec answers modify-package with {upgrading: bool} and
	// lists "upgrading" in ResourceStatusEnum, but does not say whether the
	// server must be stopped first or how long the change takes. The
	// provider waits for the status to return to "active" and then checks
	// that the package changed.
	if !plan.Package.Equal(state.Package) {
		_, httpResp, err := r.Client.Cloud().CloudServersModifyPackageCreate(ctx, id).
			ServerProductUpgradeRequest(*sdk.NewServerProductUpgradeRequest(plan.Package.ValueString())).Execute()
		if err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error changing server package", err, httpResp)
			return
		}
		detail, err := waitForActive(ctx, r.Client, id, "the package change")
		if err != nil {
			resp.Diagnostics.AddError("Server package change did not complete", err.Error())
			return
		}
		same, err := samePackage(ctx, r.Client, plan.Package.ValueString(), detail.GetPackage())
		if err != nil {
			resp.Diagnostics.AddError("Error comparing server packages", err.Error())
			return
		}
		if !same {
			resp.Diagnostics.AddError("Server package change did not take effect",
				fmt.Sprintf("Requested package %q, but the API reports %q after the change completed.",
					plan.Package.ValueString(), detail.GetPackage()))
			return
		}
	}

	// Firewall: POST public-interface.
	if !plan.FirewallRulesSetID.Equal(state.FirewallRulesSetID) ||
		(tfutil.Known(plan.FirewallPolicyIn) && !plan.FirewallPolicyIn.Equal(state.FirewallPolicyIn)) ||
		(tfutil.Known(plan.FirewallPolicyOut) && !plan.FirewallPolicyOut.Equal(state.FirewallPolicyOut)) {
		if err := setFirewall(ctx, r.Client, id, plan.FirewallRulesSetID.ValueString(),
			plan.FirewallPolicyIn.ValueString(), plan.FirewallPolicyOut.ValueString()); err != nil {
			resp.Diagnostics.AddError("Error updating server firewall", err.Error())
			return
		}
	}

	if !plan.DestroyProtection.Equal(state.DestroyProtection) {
		if err := r.setDestroyProtection(ctx, id, plan.DestroyProtection.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Error changing destroy protection", err.Error())
			return
		}
	}

	if tfutil.Known(plan.PowerState) && !plan.PowerState.Equal(state.PowerState) {
		if err := setPower(ctx, r.Client, id, plan.PowerState.ValueString(), shutdownGracePeriod); err != nil {
			resp.Diagnostics.AddError("Error changing power state", err.Error())
			return
		}
	}

	gone, d := r.refresh(ctx, &plan, false)
	resp.Diagnostics.Append(d...)
	if gone {
		resp.Diagnostics.AddError("Server disappeared", fmt.Sprintf("Server %d was deleted while it was being updated.", id))
		return
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, defaultDeleteTimeout)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	if state.DestroyProtection.ValueBool() {
		resp.Diagnostics.AddError("Server is protected from destruction",
			fmt.Sprintf("Server %d has destroy_protection enabled. Set destroy_protection = false and apply before destroying it.", id))
		return
	}

	httpResp, err := r.Client.Cloud().CloudServersDestroy(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting server", err, httpResp)
		return
	}
	// Wait until the server is gone, so that resources it used (IPs,
	// volumes, networks, firewall sets) can be deleted right after.
	if err := waitForGone(ctx, r.Client, id); err != nil && !errors.Is(err, context.Canceled) {
		resp.Diagnostics.AddError("Server deletion did not complete", err.Error())
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	tfutil.MarkImported(ctx, resp)
}
