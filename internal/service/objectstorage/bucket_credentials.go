// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package objectstorage

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/tfutil"
)

var (
	_ resource.ResourceWithConfigure   = (*credentialsResource)(nil)
	_ resource.ResourceWithImportState = (*credentialsResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*credentialsResource)(nil)
)

type credentialsResource struct{ tfutil.ResourceBase }

// NewBucketCredentialsResource returns the pidginhost_bucket_credentials resource.
func NewBucketCredentialsResource() resource.Resource { return &credentialsResource{} }

type credentialsModel struct {
	ID              types.String   `tfsdk:"id"`
	BucketID        types.String   `tfsdk:"bucket_id"`
	RotationTrigger types.String   `tfsdk:"rotation_trigger"`
	AccessKey       types.String   `tfsdk:"access_key"`
	SecretKey       types.String   `tfsdk:"secret_key"`
	Bucket          types.String   `tfsdk:"bucket"`
	Endpoint        types.String   `tfsdk:"endpoint"`
	Region          types.String   `tfsdk:"region"`
	Timeouts        timeouts.Value `tfsdk:"timeouts"`
}

func (r *credentialsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket_credentials"
}

func (r *credentialsResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The S3 access key and secret key of a bucket, read with the API's `credentials/reveal` " +
			"action.\n\n" +
			"~> **The keys are stored in the Terraform state in plain text.** They are marked sensitive, so they are " +
			"hidden in plan output, but anyone who can read the state can read them. Use an encrypted remote state " +
			"backend (or OpenTofu state encryption) and restrict access to it.\n\n" +
			"Changing `rotation_trigger` (to any new value) rotates the keys with `credentials/rotate`; the old keys " +
			"stop working. Every refresh reveals the current keys, so keys rotated outside Terraform are picked up. " +
			"Destroying this resource only removes the keys from the state; it does not revoke them.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Same as `bucket_id`.", PlanModifiers: stable},
			"bucket_id": schema.StringAttribute{
				Required: true, MarkdownDescription: "ID of the bucket.",
				Validators:    []validator.String{tfutil.IsID()},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"rotation_trigger": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Any string. Any change to it after creation (including setting or removing it) " +
					"rotates the keys, for example `rotation_trigger = time_rotating.keys.id` or a date. " +
					"Creating or importing the resource never rotates.",
			},
			"access_key": schema.StringAttribute{
				Computed: true, Sensitive: true, MarkdownDescription: "S3 access key ID.", PlanModifiers: stable,
			},
			"secret_key": schema.StringAttribute{
				Computed: true, Sensitive: true, MarkdownDescription: "S3 secret access key.", PlanModifiers: stable,
			},
			"bucket":   schema.StringAttribute{Computed: true, MarkdownDescription: "Full bucket name.", PlanModifiers: stable},
			"endpoint": schema.StringAttribute{Computed: true, MarkdownDescription: "S3 endpoint URL.", PlanModifiers: stable},
			"region":   schema.StringAttribute{Computed: true, MarkdownDescription: "S3 region.", PlanModifiers: stable},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Read: true, Update: true}),
		},
	}
}

// rotates reports whether going from prior to next rotation_trigger rotates
// the keys. Any change does, including setting or removing the value.
func rotates(prior, next types.String) bool {
	return !next.IsUnknown() && !prior.Equal(next)
}

// ModifyPlan marks the keys unknown when the plan will rotate them.
func (r *credentialsResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state credentialsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.RotationTrigger.IsUnknown() ||
		(rotates(state.RotationTrigger, plan.RotationTrigger) && !tfutil.WasImported(ctx, req.Private)) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("access_key"), types.StringUnknown())...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("secret_key"), types.StringUnknown())...)
	}
}

func applyCredentials(m *credentialsModel, c *sdk.BucketCredentials) {
	m.ID = m.BucketID
	m.AccessKey = types.StringValue(c.GetAccessKey())
	m.SecretKey = types.StringValue(c.GetSecretKey())
	m.Bucket = types.StringValue(c.GetBucket())
	m.Endpoint = types.StringValue(c.GetEndpoint())
	m.Region = types.StringValue(c.GetRegion())
}

func (r *credentialsResource) reveal(ctx context.Context, m *credentialsModel) (gone bool, err error) {
	id, err := apiclient.ParseID(m.BucketID.ValueString())
	if err != nil {
		return false, err
	}
	c, httpResp, err := r.Client.Cloud().CloudBucketsCredentialsRevealCreate(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return true, nil
	}
	if err != nil {
		return false, &apiError{apiclient.ErrorDetail(err, httpResp)}
	}
	applyCredentials(m, c)
	return false, nil
}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

func (r *credentialsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan credentialsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gone, err := r.reveal(ctx, &plan)
	if gone {
		resp.Diagnostics.AddError("Bucket not found", "Bucket "+plan.BucketID.ValueString()+" does not exist.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error revealing bucket credentials", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *credentialsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state credentialsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := state.Timeouts.Read(ctx, 2*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gone, err := r.reveal(ctx, &state)
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error revealing bucket credentials", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *credentialsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state credentialsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Update(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Right after an import the trigger is unknown to the provider: adopt
	// the configured value instead of rotating live keys.
	if rotates(state.RotationTrigger, plan.RotationTrigger) && !tfutil.WasImported(ctx, req.Private) {
		id, err := apiclient.ParseID(plan.BucketID.ValueString())
		if err != nil {
			tfutil.AddIDError(&resp.Diagnostics, err)
			return
		}
		c, httpResp, err := r.Client.Cloud().CloudBucketsCredentialsRotateCreate(ctx, id).Execute()
		if err != nil {
			tfutil.AddAPIError(&resp.Diagnostics, "Error rotating bucket credentials", err, httpResp)
			return
		}
		applyCredentials(&plan, c)
	} else if _, err := r.reveal(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error revealing bucket credentials", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	tfutil.ClearImported(ctx, resp)
}

// Delete removes the credentials from state. The API has no way to revoke
// them other than rotating, which would break whatever still uses them.
func (r *credentialsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *credentialsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := apiclient.ParseID(req.ID); err != nil {
		tfutil.AddIDError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("bucket_id"), req.ID)...)
	tfutil.MarkImported(ctx, resp)
}
