// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package server

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

// Power states accepted in configuration.
const (
	powerRunning = "running"
	powerStopped = "stopped"
)

var firewallPolicies = []string{"ACCEPT", "DROP", "REJECT"}

type serverModel struct {
	ID       types.String `tfsdk:"id"`
	Hostname types.String `tfsdk:"hostname"`
	Image    types.String `tfsdk:"image"`
	Package  types.String `tfsdk:"package"`

	Generation types.String `tfsdk:"generation"`
	Project    types.String `tfsdk:"project"`
	EnableHA   types.Bool   `tfsdk:"enable_ha"`

	Password     types.String `tfsdk:"password"`
	SSHPublicKey types.String `tfsdk:"ssh_public_key"`
	SSHKeyID     types.String `tfsdk:"ssh_key_id"`
	UserData     types.String `tfsdk:"user_data"`

	PublicIPv4            types.String `tfsdk:"public_ipv4"`
	NewIPv4               types.Bool   `tfsdk:"new_ipv4"`
	PublicIPv6            types.String `tfsdk:"public_ipv6"`
	NewIPv6               types.Bool   `tfsdk:"new_ipv6"`
	NoNetworkAcknowledged types.Bool   `tfsdk:"no_network_acknowledged"`

	PrivateNetworkID types.String `tfsdk:"private_network_id"`
	PrivateAddress   types.String `tfsdk:"private_address"`

	ExtraVolumeProduct types.String `tfsdk:"extra_volume_product"`
	ExtraVolumeSize    types.Int64  `tfsdk:"extra_volume_size"`

	FirewallRulesSetID types.String `tfsdk:"firewall_rules_set_id"`
	FirewallPolicyIn   types.String `tfsdk:"firewall_policy_in"`
	FirewallPolicyOut  types.String `tfsdk:"firewall_policy_out"`

	DestroyProtection types.Bool   `tfsdk:"destroy_protection"`
	PowerState        types.String `tfsdk:"power_state"`

	Status          types.String `tfsdk:"status"`
	ImageName       types.String `tfsdk:"image_name"`
	CPUs            types.Int64  `tfsdk:"cpus"`
	Memory          types.Int64  `tfsdk:"memory"`
	DiskSize        types.Int64  `tfsdk:"disk_size"`
	Username        types.String `tfsdk:"username"`
	IPv4Address     types.String `tfsdk:"ipv4_address"`
	IPv6Address     types.String `tfsdk:"ipv6_address"`
	PrivateNetworks types.List   `tfsdk:"private_networks"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

var privateNetworkObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"network":   types.StringType,
	"address":   types.StringType,
	"interface": types.StringType,
}}

func useState() []planmodifier.String {
	return []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
}

func serverSchema(ctx context.Context) schema.Schema {
	createOnly := []planmodifier.String{tfutil.CreateOnlyString()}
	createOnlyBool := []planmodifier.Bool{tfutil.CreateOnlyBool()}

	return schema.Schema{
		MarkdownDescription: "A cloud server.\n\n" +
			"Creation is asynchronous: the provider waits until the server's status is `active` and fails with a clear " +
			"error if it becomes `failed`. The server ID is saved as soon as the API accepts the request, so a server " +
			"that fails to provision is tainted and replaced on the next apply instead of being left behind.\n\n" +
			"Attributes marked *create-only* are sent when the server is created and cannot be changed afterwards; " +
			"changing them replaces the server. The API never returns most of them, so after `terraform import` they " +
			"can be added to the configuration without forcing a replacement.\n\n" +
			"Firewall rules, extra volumes, additional IPs and private network memberships have their own resources " +
			"(`pidginhost_firewall_rule`, `pidginhost_volume`, `pidginhost_ipv4_attachment`, " +
			"`pidginhost_private_network_attachment`). The create-only network and volume attributes here exist for the " +
			"cases where the API needs them at creation time, such as booting with a public IP already attached.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Server ID.", PlanModifiers: useState(),
			},
			"hostname": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Hostname. When omitted, the API picks one. Changing it replaces the server " +
					"(the API cannot rename a server).",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 253)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown(),
				},
			},
			"image": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "*Create-only.* OS image, as a slug (recommended; see `pidginhost_images`) or ID. " +
					"The API's name for the installed image is exported as `image_name`.",
				PlanModifiers: createOnly,
			},
			"package": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Server package (plan), as a slug (recommended; see `pidginhost_server_packages`) or ID. " +
					"Changing it resizes the server in place through the API's `modify-package` action; the provider " +
					"waits until the server is `active` again.",
			},
			"generation": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "*Create-only.* Hardware generation slug. When omitted, the API's default is used.",
				PlanModifiers:       []planmodifier.String{tfutil.CreateOnlyString(), stringplanmodifier.UseStateForUnknown()},
			},
			"project": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Project label used to group resources in the control panel. Can be changed in place.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(200)},
				PlanModifiers:       useState(),
			},
			"enable_ha": schema.BoolAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "*Create-only.* Enable high availability (automatic restart and migration on node failure). " +
					"Reflects the API's `ha_enabled`.",
				PlanModifiers: []planmodifier.Bool{tfutil.CreateOnlyBool(), boolplanmodifier.UseStateForUnknown()},
			},
			"password": schema.StringAttribute{
				Optional: true, Sensitive: true,
				MarkdownDescription: "Password for the default user. Can be changed in place. The API never returns it, " +
					"so changes made outside Terraform are not detected. It is stored in the Terraform state.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 50)},
			},
			"ssh_public_key": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "SSH public key for the default user. Can be changed in place, but **applying a new key " +
					"regenerates cloud-init data and reboots a running server**. Setting it to `\"\"` removes the key from " +
					"future cloud-init data without revoking keys already in the guest.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(3000)},
				PlanModifiers: useState(),
			},
			"ssh_key_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* ID or fingerprint of a stored SSH key (`pidginhost_ssh_key`) to install.",
				PlanModifiers:       createOnly,
			},
			"user_data": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "*Create-only.* Startup script run on first boot (cloud-init user data). The API documents it as " +
					"an executable script with a shebang, such as `#!/bin/sh`. The API stores it in cleartext, so do not " +
					"put secrets in it.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(65536)},
				PlanModifiers: createOnly,
			},
			"public_ipv4": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* ID or address of an IPv4 you already own (`pidginhost_ipv4`), attached before first boot.",
				PlanModifiers:       createOnly,
			},
			"new_ipv4": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "*Create-only.* Allocate a new IPv4 and attach it before first boot. The address is not " +
					"managed as a separate resource, and the API does not document whether it is released when the server " +
					"is deleted. To control the address's lifetime, create a `pidginhost_ipv4` and pass it as `public_ipv4`.",
				PlanModifiers: createOnlyBool,
			},
			"public_ipv6": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* ID or address of an IPv6 you already own (`pidginhost_ipv6`), attached before first boot.",
				PlanModifiers:       createOnly,
			},
			"new_ipv6": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* Allocate a new IPv6 and attach it before first boot. As with `new_ipv4`, " +
					"the address is not managed as a separate resource.",
				PlanModifiers:       createOnlyBool,
			},
			"no_network_acknowledged": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* Confirm that the server is intentionally created without a public IP.",
				PlanModifiers:       createOnlyBool,
			},
			"private_network_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "*Create-only.* ID of a private network to join at creation. Prefer " +
					"`pidginhost_private_network_attachment`, which can be changed later; do not use both for the same network.",
				PlanModifiers: createOnly,
			},
			"private_address": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* Address in `private_network_id`. Leave empty for automatic assignment.",
				Validators: []validator.String{
					tfutil.IsIPv4(),
					stringvalidator.AlsoRequires(path.MatchRoot("private_network_id")),
				},
				PlanModifiers: createOnly,
			},
			"extra_volume_product": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "*Create-only.* Storage product of an extra volume created with the server. Prefer " +
					"`pidginhost_volume`, which can be resized and is tracked in state.",
				Validators:    []validator.String{stringvalidator.AlsoRequires(path.MatchRoot("extra_volume_size"))},
				PlanModifiers: createOnly,
			},
			"extra_volume_size": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "*Create-only.* Size in GB of the extra volume.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
					int64validator.AlsoRequires(path.MatchRoot("extra_volume_product")),
				},
				PlanModifiers: []planmodifier.Int64{tfutil.CreateOnlyInt64()},
			},
			"firewall_rules_set_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "ID of the `pidginhost_firewall_rules_set` applied to the server's public interface. " +
					"It is applied at creation, before first boot, and can be changed or removed in place.",
				Validators: []validator.String{tfutil.IsID()},
			},
			"firewall_policy_in": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Default policy for inbound traffic not matched by a rule: `ACCEPT`, `DROP` or `REJECT`. " +
					"The API default is `ACCEPT`. Can be changed in place.",
				Validators:    []validator.String{stringvalidator.OneOf(firewallPolicies...)},
				PlanModifiers: useState(),
			},
			"firewall_policy_out": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Default policy for outbound traffic not matched by a rule: `ACCEPT`, `DROP` or `REJECT`. " +
					"The API default is `ACCEPT`. Can be changed in place.",
				Validators:    []validator.String{stringvalidator.OneOf(firewallPolicies...)},
				PlanModifiers: useState(),
			},
			"destroy_protection": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Prevent the server from being destroyed. While `true`, `terraform destroy` fails; set it " +
					"to `false` and apply before destroying. Defaults to `false`.",
			},
			"power_state": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Desired power state: `running` or `stopped`. Stopping uses a graceful shutdown and " +
					"falls back to a hard stop if the guest does not power off. When omitted, the power state is " +
					"reported but not managed.",
				Validators:    []validator.String{stringvalidator.OneOf(powerRunning, powerStopped)},
				PlanModifiers: useState(),
			},
			"status": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Lifecycle status reported by the API (`active`, `provisioning`, ...).",
				PlanModifiers: useState(),
			},
			"image_name": schema.StringAttribute{
				Computed: true, MarkdownDescription: "The installed image, as reported by the API.",
				PlanModifiers: useState(),
			},
			"cpus": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Number of vCPUs.",
			},
			"memory": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Memory, as reported by the API.",
			},
			"disk_size": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Boot disk size, as reported by the API.",
			},
			"username": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Default login user.",
				PlanModifiers: useState(),
			},
			"ipv4_address": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Primary public IPv4 address, if any.",
			},
			"ipv6_address": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Primary public IPv6 address, if any.",
			},
			"private_networks": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Private network interfaces of the server.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"network":   schema.StringAttribute{Computed: true, MarkdownDescription: "Network, as reported by the API."},
					"address":   schema.StringAttribute{Computed: true, MarkdownDescription: "Address of the server in the network."},
					"interface": schema.StringAttribute{Computed: true, MarkdownDescription: "Interface name."},
				}},
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}
