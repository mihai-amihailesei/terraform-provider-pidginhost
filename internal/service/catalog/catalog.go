// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package catalog implements read-only data sources for the cloud catalog:
// OS images, server packages and storage products.
package catalog

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

// ---------------------------------------------------------------- images

type imagesDataSource struct{ tfutil.DataSourceBase }

// NewImagesDataSource returns the pidginhost_images data source.
func NewImagesDataSource() datasource.DataSource { return &imagesDataSource{} }

type imagesModel struct {
	FamilyName types.String `tfsdk:"family_name"`
	Images     []imageModel `tfsdk:"images"`
}

type imageModel struct {
	ID              types.String `tfsdk:"id"`
	Slug            types.String `tfsdk:"slug"`
	Name            types.String `tfsdk:"name"`
	FamilyName      types.String `tfsdk:"family_name"`
	IsDefault       types.Bool   `tfsdk:"is_default"`
	DefaultUsername types.String `tfsdk:"default_username"`
}

func (d *imagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_images"
}

func (d *imagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the OS images available for servers. Use an image's `slug` as the `image` of a `pidginhost_server`.",
		Attributes: map[string]schema.Attribute{
			"family_name": schema.StringAttribute{
				MarkdownDescription: "Only return images of this family, for example `Ubuntu` (case-sensitive).",
				Optional:            true,
			},
			"images": schema.ListNestedAttribute{
				MarkdownDescription: "The matching images.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":               schema.StringAttribute{Computed: true, MarkdownDescription: "Image ID."},
					"slug":             schema.StringAttribute{Computed: true, MarkdownDescription: "Image slug, for example `ubuntu-24-04`."},
					"name":             schema.StringAttribute{Computed: true, MarkdownDescription: "Display name."},
					"family_name":      schema.StringAttribute{Computed: true, MarkdownDescription: "Image family, or null."},
					"is_default":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this is the default version within its family."},
					"default_username": schema.StringAttribute{Computed: true, MarkdownDescription: "Account the image provisions for SSH login."},
				}},
			},
		},
	}
}

func (d *imagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg imagesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.OSImage, bool, *http.Response, error) {
		res, httpResp, err := d.Client.Cloud().CloudImagesList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, httpResp, err
		}
		return res.GetResults(), res.GetNext() != "", httpResp, nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing images", err.Error())
		return
	}
	cfg.Images = []imageModel{}
	for _, img := range all {
		if tfutil.Known(cfg.FamilyName) && img.GetFamilyName() != cfg.FamilyName.ValueString() {
			continue
		}
		cfg.Images = append(cfg.Images, imageModel{
			ID:              types.StringValue(apiclient.FormatID(img.GetId())),
			Slug:            types.StringValue(img.GetSlug()),
			Name:            types.StringValue(img.GetName()),
			FamilyName:      tfutil.StringOrNull(img.GetFamilyName()),
			IsDefault:       types.BoolValue(img.GetIsDefault()),
			DefaultUsername: types.StringValue(img.GetDefaultUsername()),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// ------------------------------------------------------- server packages

type packagesDataSource struct{ tfutil.DataSourceBase }

// NewServerPackagesDataSource returns the pidginhost_server_packages data source.
func NewServerPackagesDataSource() datasource.DataSource { return &packagesDataSource{} }

type packagesModel struct {
	Generation types.String   `tfsdk:"generation"`
	Packages   []packageModel `tfsdk:"packages"`
}

type packageModel struct {
	ID                   types.String `tfsdk:"id"`
	Slug                 types.String `tfsdk:"slug"`
	Name                 types.String `tfsdk:"name"`
	CPUs                 types.Int64  `tfsdk:"cpus"`
	Memory               types.Int64  `tfsdk:"memory"`
	DiskSize             types.Int64  `tfsdk:"disk_size"`
	Traffic              types.Int64  `tfsdk:"traffic"`
	AvailableGenerations types.List   `tfsdk:"available_generations"`
}

func (d *packagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_packages"
}

func (d *packagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the server packages (plans). Use a package's `slug` as the `package` of a `pidginhost_server`.",
		Attributes: map[string]schema.Attribute{
			"generation": schema.StringAttribute{
				MarkdownDescription: "Only return packages available on this hardware generation (slug).",
				Optional:            true,
			},
			"packages": schema.ListNestedAttribute{
				MarkdownDescription: "The matching packages.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":        schema.StringAttribute{Computed: true, MarkdownDescription: "Package ID."},
					"slug":      schema.StringAttribute{Computed: true, MarkdownDescription: "Package slug."},
					"name":      schema.StringAttribute{Computed: true, MarkdownDescription: "Display name."},
					"cpus":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of vCPUs."},
					"memory":    schema.Int64Attribute{Computed: true, MarkdownDescription: "Memory, as reported by the API."},
					"disk_size": schema.Int64Attribute{Computed: true, MarkdownDescription: "Boot disk size, as reported by the API."},
					"traffic":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Included monthly traffic, in TB."},
					"available_generations": schema.ListAttribute{
						Computed: true, ElementType: types.StringType,
						MarkdownDescription: "Hardware generations this package can be ordered on.",
					},
				}},
			},
		},
	}
}

func (d *packagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg packagesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.ServerProduct, bool, *http.Response, error) {
		r := d.Client.Cloud().CloudServerPackagesList(ctx).Page(page)
		if tfutil.Known(cfg.Generation) {
			r = r.Generation(cfg.Generation.ValueString())
		}
		res, httpResp, err := r.Execute()
		if err != nil {
			return nil, false, httpResp, err
		}
		return res.GetResults(), res.GetNext() != "", httpResp, nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing server packages", err.Error())
		return
	}
	cfg.Packages = []packageModel{}
	for _, p := range all {
		gens, diags := types.ListValueFrom(ctx, types.StringType, p.GetAvailableGenerations())
		resp.Diagnostics.Append(diags...)
		cfg.Packages = append(cfg.Packages, packageModel{
			ID:                   types.StringValue(apiclient.FormatID(p.GetId())),
			Slug:                 types.StringValue(p.GetSlug()),
			Name:                 types.StringValue(p.GetName()),
			CPUs:                 types.Int64Value(int64(p.GetCpus())),
			Memory:               types.Int64Value(int64(p.GetMemory())),
			DiskSize:             types.Int64Value(int64(p.GetDiskSize())),
			Traffic:              types.Int64Value(int64(p.GetTraffic())),
			AvailableGenerations: gens,
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// ------------------------------------------------------ storage products

type storageProductsDataSource struct{ tfutil.DataSourceBase }

// NewStorageProductsDataSource returns the pidginhost_storage_products data source.
func NewStorageProductsDataSource() datasource.DataSource { return &storageProductsDataSource{} }

type storageProductsModel struct {
	Products []storageProductModel `tfsdk:"products"`
}

type storageProductModel struct {
	ID      types.String `tfsdk:"id"`
	Slug    types.String `tfsdk:"slug"`
	Name    types.String `tfsdk:"name"`
	Type    types.String `tfsdk:"type"`
	Unit    types.String `tfsdk:"unit"`
	Price   types.String `tfsdk:"price"`
	MinSize types.Int64  `tfsdk:"min_size"`
	MaxSize types.Int64  `tfsdk:"max_size"`
}

func (d *storageProductsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_products"
}

func (d *storageProductsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the storage products. Use a product's `slug` as the `product` of a `pidginhost_volume`.",
		Attributes: map[string]schema.Attribute{
			"products": schema.ListNestedAttribute{
				MarkdownDescription: "The storage products.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":       schema.StringAttribute{Computed: true, MarkdownDescription: "Product ID."},
					"slug":     schema.StringAttribute{Computed: true, MarkdownDescription: "Product slug."},
					"name":     schema.StringAttribute{Computed: true, MarkdownDescription: "Display name."},
					"type":     schema.StringAttribute{Computed: true, MarkdownDescription: "Product type."},
					"unit":     schema.StringAttribute{Computed: true, MarkdownDescription: "Billing unit."},
					"price":    schema.StringAttribute{Computed: true, MarkdownDescription: "Price per unit per month, as a decimal string."},
					"min_size": schema.Int64Attribute{Computed: true, MarkdownDescription: "Minimum size."},
					"max_size": schema.Int64Attribute{Computed: true, MarkdownDescription: "Maximum size."},
				}},
			},
		},
	}
}

func (d *storageProductsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.StorageProduct, bool, *http.Response, error) {
		res, httpResp, err := d.Client.Cloud().CloudStorageProductsList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, httpResp, err
		}
		return res.GetResults(), res.GetNext() != "", httpResp, nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing storage products", err.Error())
		return
	}
	state := storageProductsModel{Products: []storageProductModel{}}
	for _, p := range all {
		state.Products = append(state.Products, storageProductModel{
			ID:      types.StringValue(apiclient.FormatID(p.GetId())),
			Slug:    types.StringValue(p.GetSlug()),
			Name:    types.StringValue(p.GetName()),
			Type:    types.StringValue(p.GetType()),
			Unit:    types.StringValue(p.GetUnit()),
			Price:   types.StringValue(p.GetPrice()),
			MinSize: types.Int64Value(int64(p.GetMinSize())),
			MaxSize: types.Int64Value(int64(p.GetMaxSize())),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
