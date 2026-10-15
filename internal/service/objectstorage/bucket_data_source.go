// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package objectstorage

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/tfutil"
)

type bucketDataSource struct{ tfutil.DataSourceBase }

// NewBucketDataSource returns the pidginhost_bucket data source.
func NewBucketDataSource() datasource.DataSource { return &bucketDataSource{} }

type bucketDataModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	FullName    types.String `tfsdk:"full_name"`
	QuotaGB     types.Int64  `tfsdk:"quota_gb"`
	PublicRead  types.Bool   `tfsdk:"public_read"`
	Endpoint    types.String `tfsdk:"endpoint"`
	Region      types.String `tfsdk:"region"`
	Status      types.String `tfsdk:"status"`
	UsedBytes   types.Int64  `tfsdk:"used_bytes"`
	ObjectCount types.Int64  `tfsdk:"object_count"`
	Created     types.String `tfsdk:"created"`
}

func (d *bucketDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket"
}

func (d *bucketDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	num := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing bucket by `id` or `name` (exactly one).",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Bucket ID."},
			"name":         schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Bucket name (as given at creation) or full name."},
			"full_name":    str("Full bucket name to use with S3 clients."),
			"quota_gb":     num("Storage quota in GB."),
			"public_read":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether anonymous read access is allowed."},
			"endpoint":     str("S3 endpoint URL."),
			"region":       str("S3 region."),
			"status":       str("Lifecycle status."),
			"used_bytes":   num("Bytes stored."),
			"object_count": num("Number of objects."),
			"created":      str("Creation time."),
		},
	}
}

func (d *bucketDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *bucketDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg bucketDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var b *apiclient.Bucket
	if tfutil.Known(cfg.ID) {
		id, err := apiclient.ParseID(cfg.ID.ValueString())
		if err != nil {
			tfutil.AddIDError(&resp.Diagnostics, err)
			return
		}
		got, httpResp, err := d.Client.GetBucket(ctx, id)
		if err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error reading bucket", err, httpResp)
			return
		}
		b = got
	} else {
		all, httpResp, err := d.Client.ListBuckets(ctx)
		if err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error listing buckets", err, httpResp)
			return
		}
		name := cfg.Name.ValueString()
		for i := range all {
			if (all[i].Name == name || all[i].FullName == name) && !apiclient.IsGoneStatus(all[i].Status) {
				if b != nil {
					resp.Diagnostics.AddError("Ambiguous bucket", fmt.Sprintf("More than one bucket is named %q; use id.", name))
					return
				}
				b = &all[i]
			}
		}
		if b == nil {
			resp.Diagnostics.AddError("Bucket not found", fmt.Sprintf("No bucket is named %q.", name))
			return
		}
	}

	cfg.ID = types.StringValue(apiclient.FormatID(b.ID))
	cfg.Name = types.StringValue(b.Name)
	cfg.FullName = types.StringValue(b.FullName)
	cfg.QuotaGB = types.Int64Value(b.QuotaGB)
	cfg.PublicRead = types.BoolValue(b.PublicRead)
	cfg.Endpoint = types.StringValue(b.Endpoint)
	cfg.Region = types.StringValue(b.Region)
	cfg.Status = types.StringValue(b.Status)
	cfg.UsedBytes = types.Int64Value(b.UsedBytes)
	cfg.ObjectCount = types.Int64Value(b.ObjectCount)
	cfg.Created = types.StringValue(b.Created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
