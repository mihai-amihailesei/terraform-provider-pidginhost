// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package objectstorage implements S3-compatible object storage buckets and
// their credentials.
package objectstorage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*bucketResource)(nil)
	_ resource.ResourceWithImportState = (*bucketResource)(nil)
)

type bucketResource struct{ tfutil.ResourceBase }

// NewBucketResource returns the pidginhost_bucket resource.
func NewBucketResource() resource.Resource { return &bucketResource{} }

type bucketModel struct {
	ID          types.String   `tfsdk:"id"`
	Name        types.String   `tfsdk:"name"`
	QuotaGB     types.Int64    `tfsdk:"quota_gb"`
	PublicRead  types.Bool     `tfsdk:"public_read"`
	FullName    types.String   `tfsdk:"full_name"`
	Endpoint    types.String   `tfsdk:"endpoint"`
	Region      types.String   `tfsdk:"region"`
	Status      types.String   `tfsdk:"status"`
	UsedBytes   types.Int64    `tfsdk:"used_bytes"`
	ObjectCount types.Int64    `tfsdk:"object_count"`
	Created     types.String   `tfsdk:"created"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func (r *bucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket"
}

func (r *bucketResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An S3-compatible object storage bucket. Creation and deletion are asynchronous; the " +
			"provider waits for them to finish. Get access keys with `pidginhost_bucket_credentials`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Bucket ID.", PlanModifiers: stable},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Bucket name, up to 63 characters. The API prefixes it to build `full_name`. " +
					"Changing it replaces the bucket and **deletes its contents**.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 63)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"quota_gb": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Storage quota in GB. Changed in place with the API's `resize` action.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"public_read": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Allow anonymous read access to objects. Changed in place with the API's " +
					"`visibility` action. Defaults to `false`.",
			},
			"full_name": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Full bucket name to use with S3 clients.", PlanModifiers: stable,
			},
			"endpoint": schema.StringAttribute{
				Computed: true, MarkdownDescription: "S3 endpoint URL.", PlanModifiers: stable,
			},
			"region": schema.StringAttribute{
				Computed: true, MarkdownDescription: "S3 region.", PlanModifiers: stable,
			},
			"status": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Lifecycle status (`active`, ...).",
			},
			"used_bytes": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Bytes stored, at the last refresh.",
			},
			"object_count": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Number of objects, at the last refresh.",
			},
			"created": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Creation time.", PlanModifiers: stable,
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
		},
	}
}

func applyBucket(m *bucketModel, b *apiclient.Bucket) {
	m.ID = types.StringValue(apiclient.FormatID(b.ID))
	// The name cannot change; keep the configured spelling if the API
	// returns it in another case, so normalisation never forces a
	// replacement.
	if !tfutil.Known(m.Name) || !strings.EqualFold(m.Name.ValueString(), b.Name) {
		m.Name = types.StringValue(b.Name)
	}
	m.QuotaGB = types.Int64Value(b.QuotaGB)
	m.PublicRead = types.BoolValue(b.PublicRead)
	m.FullName = types.StringValue(b.FullName)
	m.Endpoint = types.StringValue(b.Endpoint)
	m.Region = types.StringValue(b.Region)
	m.Status = types.StringValue(b.Status)
	m.UsedBytes = types.Int64Value(b.UsedBytes)
	m.ObjectCount = types.Int64Value(b.ObjectCount)
	m.Created = types.StringValue(b.Created)
}

// waitBucket polls until the bucket's status is "active".
//
// The spec answers POST with 202 and the bucket, whose status is a
// ResourceStatusEnum, but does not list the transitions. As for servers,
// statuses that cannot lead to "active" are treated as failures.
func waitBucket(ctx context.Context, c *apiclient.Client, id int32) (*apiclient.Bucket, error) {
	var last *apiclient.Bucket
	err := apiclient.WaitFor(ctx, c.PollInterval, fmt.Sprintf("bucket %d to become active", id),
		func(ctx context.Context) (bool, string, error) {
			b, httpResp, err := c.GetBucket(ctx, id)
			if err != nil {
				return false, "", fmt.Errorf("reading bucket %d: %s", id, apiclient.ErrorDetail(err, httpResp))
			}
			last = b
			switch {
			case b.Status == apiclient.StatusActive:
				return true, b.Status, nil
			case apiclient.IsTerminalFailure(b.Status):
				return false, b.Status, fmt.Errorf("%w: bucket %d reached status %q", apiclient.ErrWaitFailed, id, b.Status)
			default:
				return false, b.Status, nil
			}
		})
	return last, err
}

func (r *bucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	b, httpResp, err := r.Client.CreateBucket(ctx, plan.Name.ValueString(), int32(plan.QuotaGB.ValueInt64()), plan.PublicRead.ValueBool())
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error creating bucket", err, httpResp)
		return
	}
	// Track the bucket before waiting, so a failed creation is tainted
	// rather than orphaned.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), apiclient.FormatID(b.ID))...)

	ready, err := waitBucket(ctx, r.Client, b.ID)
	if err != nil {
		resp.Diagnostics.AddError("Bucket did not become active", err.Error())
		if ready != nil {
			applyBucket(&plan, ready)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		}
		return
	}
	applyBucket(&plan, ready)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bucketModel
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
	b, httpResp, err := r.Client.GetBucket(ctx, id)
	if apiclient.IsNotFound(httpResp) || (err == nil && apiclient.IsGoneStatus(b.Status)) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error reading bucket", err, httpResp)
		return
	}
	applyBucket(&state, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *bucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state bucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	if !plan.QuotaGB.Equal(state.QuotaGB) {
		if _, httpResp, err := r.Client.ResizeBucket(ctx, id, int32(plan.QuotaGB.ValueInt64())); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error resizing bucket", err, httpResp)
			return
		}
	}
	if !plan.PublicRead.Equal(state.PublicRead) {
		if _, httpResp, err := r.Client.SetBucketVisibility(ctx, id, plan.PublicRead.ValueBool()); err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error changing bucket visibility", err, httpResp)
			return
		}
	}
	// Assumption: the spec answers resize and visibility with 200 and the
	// bucket, so they are taken to be synchronous; waiting for "active"
	// covers the case where they are not.
	b, err := waitBucket(ctx, r.Client, id)
	if err != nil {
		resp.Diagnostics.AddError("Bucket update did not complete", err.Error())
		return
	}
	applyBucket(&plan, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Delete(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := apiclient.ParseID(state.ID.ValueString())
	if err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	_, httpResp, err := r.Client.Cloud().CloudBucketsDestroy(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return
	}
	if err != nil {
		tfutil.AddAPIError(&resp.Diagnostics, "Error deleting bucket", err, httpResp)
		return
	}
	// DELETE answers 202 with the status the bucket moved to: deletion is
	// asynchronous. Wait until the bucket is gone or cancelled/terminated.
	err = apiclient.WaitFor(ctx, r.Client.PollInterval, fmt.Sprintf("bucket %d to be deleted", id),
		func(ctx context.Context) (bool, string, error) {
			b, httpResp, err := r.Client.GetBucket(ctx, id)
			if apiclient.IsNotFound(httpResp) {
				return true, "", nil
			}
			if err != nil {
				return false, "", fmt.Errorf("%s", apiclient.ErrorDetail(err, httpResp))
			}
			return apiclient.IsGoneStatus(b.Status), b.Status, nil
		})
	if err != nil && !errors.Is(err, context.Canceled) {
		resp.Diagnostics.AddError("Bucket deletion did not complete", err.Error())
	}
}

func (r *bucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
