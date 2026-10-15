// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package account

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

type sshKeyDataSource struct{ tfutil.DataSourceBase }

// NewSSHKeyDataSource returns the pidginhost_ssh_key data source.
func NewSSHKeyDataSource() datasource.DataSource { return &sshKeyDataSource{} }

type sshKeyDataModel struct {
	ID          types.String `tfsdk:"id"`
	Alias       types.String `tfsdk:"alias"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	PublicKey   types.String `tfsdk:"public_key"`
}

func (d *sshKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (d *sshKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an SSH key stored in the account by `id`, `alias` or `fingerprint` (exactly one).",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "SSH key ID."},
			"alias":       schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Alias of the key. It must match exactly one key."},
			"fingerprint": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Fingerprint of the key."},
			"public_key":  schema.StringAttribute{Computed: true, MarkdownDescription: "The public key."},
		},
	}
}

func (d *sshKeyDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("alias"), path.MatchRoot("fingerprint")),
	}
}

func (d *sshKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sshKeyDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var key *sdk.SSHKey
	if tfutil.Known(cfg.ID) {
		k, httpResp, err := d.Client.Account().AccountSshKeysRetrieve(ctx, cfg.ID.ValueString()).Execute()
		if err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error reading SSH key", err, httpResp)
			return
		}
		key = k
	} else {
		all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.SSHKey, bool, *http.Response, error) {
			res, httpResp, err := d.Client.Account().AccountSshKeysList(ctx).Page(page).Execute()
			if err != nil {
				return nil, false, httpResp, err
			}
			return res.GetResults(), res.GetNext() != "", httpResp, nil
		})
		if err != nil {
			resp.Diagnostics.AddError("Error listing SSH keys", err.Error())
			return
		}
		var matches []sdk.SSHKey
		for _, k := range all {
			if (tfutil.Known(cfg.Alias) && k.GetAlias() == cfg.Alias.ValueString()) ||
				(tfutil.Known(cfg.Fingerprint) && k.GetFingerprint() == cfg.Fingerprint.ValueString()) {
				matches = append(matches, k)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("SSH key not found", "No SSH key in the account matches the given alias or fingerprint.")
			return
		case 1:
			key = &matches[0]
		default:
			resp.Diagnostics.AddError("Ambiguous SSH key", fmt.Sprintf("%d SSH keys match; look the key up by id instead.", len(matches)))
			return
		}
	}

	cfg.ID = types.StringValue(apiclient.FormatID(key.GetId()))
	cfg.Alias = types.StringValue(key.GetAlias())
	cfg.Fingerprint = types.StringValue(key.GetFingerprint())
	cfg.PublicKey = types.StringValue(key.GetKey())
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
