// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package volume implements block storage volumes and their attachments.
package volume

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
)

// serverHasVolume reports whether volume volumeID is listed on server
// serverID. A server that does not exist has no volumes.
//
// Volume.server only holds the hostname of the server a volume is attached
// to, so the typed ServerDetail.volumes list is used to match by ID.
func serverHasVolume(ctx context.Context, c *apiclient.Client, serverID, volumeID int32) (bool, error) {
	s, httpResp, err := c.Cloud().CloudServersRetrieve(ctx, serverID).Execute()
	if apiclient.IsNotFound(httpResp) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading server %d: %s", serverID, apiclient.ErrorDetail(err, httpResp))
	}
	return slices.ContainsFunc(s.GetVolumes(), func(v sdk.Volume) bool { return v.GetId() == volumeID }), nil
}

// findServerByHostname returns the ID of the server with this hostname, or
// 0 when there is none.
func findServerByHostname(ctx context.Context, c *apiclient.Client, hostname string) (int32, error) {
	all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.Server, bool, *http.Response, error) {
		res, httpResp, err := c.Cloud().CloudServersList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, httpResp, err
		}
		return res.GetResults(), res.GetNext() != "", httpResp, nil
	})
	if err != nil {
		return 0, err
	}
	for _, s := range all {
		if strings.EqualFold(s.GetHostname(), hostname) {
			return s.GetId(), nil
		}
	}
	return 0, nil
}

// attachedServer returns the ID of the server a volume is attached to, or 0.
// prior is checked first to avoid listing every server.
func attachedServer(ctx context.Context, c *apiclient.Client, v *sdk.Volume, prior int32) (int32, error) {
	if !v.GetAttached() {
		return 0, nil
	}
	if prior != 0 {
		has, err := serverHasVolume(ctx, c, prior, v.GetId())
		if err != nil {
			return 0, err
		}
		if has {
			return prior, nil
		}
	}
	if v.GetServer() == "" {
		return 0, nil
	}
	return findServerByHostname(ctx, c, v.GetServer())
}

func attach(ctx context.Context, c *apiclient.Client, volumeID, serverID int32) error {
	_, httpResp, err := c.Cloud().CloudVolumesAttachCreate(ctx, volumeID).AttachVolumeRequest(*sdk.NewAttachVolumeRequest(serverID)).Execute()
	if err != nil {
		return fmt.Errorf("attaching volume %d to server %d: %s", volumeID, serverID, apiclient.ErrorDetail(err, httpResp))
	}
	return waitAttached(ctx, c, volumeID, true)
}

func detach(ctx context.Context, c *apiclient.Client, volumeID int32) error {
	_, httpResp, err := c.Cloud().CloudVolumesDetachCreate(ctx, volumeID).Execute()
	if err != nil && !apiclient.IsNotFound(httpResp) {
		return fmt.Errorf("detaching volume %d: %s", volumeID, apiclient.ErrorDetail(err, httpResp))
	}
	return waitAttached(ctx, c, volumeID, false)
}

// waitAttached polls until the volume's attached flag matches want.
//
// Assumption: attach and detach answer 200 with {attached}/{detached}; the
// spec does not say whether the change is immediate, so the provider polls.
func waitAttached(ctx context.Context, c *apiclient.Client, volumeID int32, want bool) error {
	return apiclient.WaitFor(ctx, c.PollInterval, fmt.Sprintf("volume %d attached=%t", volumeID, want),
		func(ctx context.Context) (bool, string, error) {
			v, httpResp, err := c.Cloud().CloudVolumesRetrieve(ctx, volumeID).Execute()
			if apiclient.IsNotFound(httpResp) && !want {
				return true, "", nil
			}
			if err != nil {
				return false, "", fmt.Errorf("reading volume %d: %s", volumeID, apiclient.ErrorDetail(err, httpResp))
			}
			return v.GetAttached() == want, fmt.Sprintf("attached=%t", v.GetAttached()), nil
		})
}
