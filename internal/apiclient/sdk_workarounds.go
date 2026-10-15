// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

// SDK WORKAROUNDS
//
// This file is the only place where the provider reads API responses itself
// instead of using the SDK's decoded models. Every workaround is listed in
// UPSTREAM_ISSUES.md and should be removed once the SDK is fixed.
//
// The requests are still built and sent by the SDK; only the response body,
// which the SDK keeps readable on the returned *http.Response, is decoded
// again here.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	sdk "github.com/pidginhost/sdk-go"
)

// Bucket is sdk.Bucket with 64-bit counters.
//
// WORKAROUND (UPSTREAM_ISSUES.md, "Bucket counters are int32 in the SDK"):
// sdk-go v0.14.0 declares Bucket.used_bytes and Bucket.object_count as
// int32 because the spec it was generated from had no format. The current
// spec declares them int64. With int32, decoding any bucket holding more than
// 2 GiB fails ("cannot unmarshal number ... into Go struct field ... of type
// int32"), which would make every refresh of such a bucket fail.
type Bucket struct {
	ID          int32  `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	QuotaGB     int64  `json:"quota_gb"`
	UsedBytes   int64  `json:"used_bytes"`
	ObjectCount int64  `json:"object_count"`
	PublicRead  bool   `json:"public_read"`
	Status      string `json:"status"`
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Created     string `json:"created"`
}

// decodeAgain decodes the raw body of an SDK response into v. It is used when
// the SDK's own decoding failed (or would fail) on a successful response.
func decodeAgain(resp *http.Response, sdkErr error, v any) error {
	if resp == nil {
		return sdkErr
	}
	if resp.StatusCode >= 300 {
		return sdkErr
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decoding response body: %w", err)
	}
	return nil
}

// GetBucket wraps CloudBucketsRetrieve.
func (c *Client) GetBucket(ctx context.Context, id int32) (*Bucket, *http.Response, error) {
	_, resp, err := c.Cloud().CloudBucketsRetrieve(ctx, id).Execute()
	var b Bucket
	if err := decodeAgain(resp, err, &b); err != nil {
		return nil, resp, err
	}
	return &b, resp, nil
}

// CreateBucket wraps CloudBucketsCreate.
func (c *Client) CreateBucket(ctx context.Context, name string, quotaGB int32, publicRead bool) (*Bucket, *http.Response, error) {
	body := sdkBucketCreateRequest(name, quotaGB, publicRead)
	_, resp, err := c.Cloud().CloudBucketsCreate(ctx).BucketCreateRequest(body).Execute()
	var b Bucket
	if err := decodeAgain(resp, err, &b); err != nil {
		return nil, resp, err
	}
	return &b, resp, nil
}

// ListBuckets wraps CloudBucketsList.
func (c *Client) ListBuckets(ctx context.Context) ([]Bucket, *http.Response, error) {
	_, resp, err := c.Cloud().CloudBucketsList(ctx).Execute()
	var out []Bucket
	if err := decodeAgain(resp, err, &out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ResizeBucket wraps CloudBucketsResizeCreate.
func (c *Client) ResizeBucket(ctx context.Context, id, quotaGB int32) (*Bucket, *http.Response, error) {
	_, resp, err := c.Cloud().CloudBucketsResizeCreate(ctx, id).BucketResizeRequest(sdkBucketResizeRequest(quotaGB)).Execute()
	var b Bucket
	if err := decodeAgain(resp, err, &b); err != nil {
		return nil, resp, err
	}
	return &b, resp, nil
}

// SetBucketVisibility wraps CloudBucketsVisibilityCreate.
func (c *Client) SetBucketVisibility(ctx context.Context, id int32, publicRead bool) (*Bucket, *http.Response, error) {
	_, resp, err := c.Cloud().CloudBucketsVisibilityCreate(ctx, id).BucketVisibilityRequest(sdkBucketVisibilityRequest(publicRead)).Execute()
	var b Bucket
	if err := decodeAgain(resp, err, &b); err != nil {
		return nil, resp, err
	}
	return &b, resp, nil
}

func sdkBucketCreateRequest(name string, quotaGB int32, publicRead bool) sdk.BucketCreateRequest {
	r := sdk.NewBucketCreateRequest(name, quotaGB)
	r.SetPublicRead(publicRead)
	return *r
}

func sdkBucketResizeRequest(quotaGB int32) sdk.BucketResizeRequest {
	return *sdk.NewBucketResizeRequest(quotaGB)
}

func sdkBucketVisibilityRequest(publicRead bool) sdk.BucketVisibilityRequest {
	return *sdk.NewBucketVisibilityRequest(publicRead)
}
