// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network

import (
	"context"
	"net/http"

	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
)

// ipInfo is the part of PublicIPv4/PublicIPv6 the provider uses. The two
// SDK types have identical fields but no common interface.
type ipInfo struct {
	ID       int32
	Slug     string
	Address  string
	Gateway  string
	Prefix   int32
	Attached bool
	ServerID *int32
}

// ipFamily binds the IPv4 or IPv6 endpoints of the SDK.
type ipFamily struct {
	name    string // "ipv4" or "ipv6"
	label   string // "IPv4" or "IPv6"
	create  func(ctx context.Context, c *apiclient.Client) (*ipInfo, *http.Response, error)
	get     func(ctx context.Context, c *apiclient.Client, id int32) (*ipInfo, *http.Response, error)
	destroy func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error)
	detach  func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error)
	getRDNS func(ctx context.Context, c *apiclient.Client, id int32) (string, *http.Response, error)
	setRDNS func(ctx context.Context, c *apiclient.Client, id int32, name string) (*http.Response, error)
	attach  func(ctx context.Context, c *apiclient.Client, serverID int32, ref string, reboot bool) (rebootRequired bool, resp *http.Response, err error)
}

func fromV4(ip *sdk.PublicIPv4) *ipInfo {
	if ip == nil {
		return nil
	}
	info := &ipInfo{ID: ip.GetId(), Slug: ip.GetSlug(), Address: ip.GetAddress(), Gateway: ip.GetGateway(),
		Prefix: ip.GetPrefix(), Attached: ip.GetAttached()}
	if v, ok := ip.GetServerIdOk(); ok && v != nil {
		id := *v
		info.ServerID = &id
	}
	return info
}

func fromV6(ip *sdk.PublicIPv6) *ipInfo {
	if ip == nil {
		return nil
	}
	info := &ipInfo{ID: ip.GetId(), Slug: ip.GetSlug(), Address: ip.GetAddress(), Gateway: ip.GetGateway(),
		Prefix: ip.GetPrefix(), Attached: ip.GetAttached()}
	if v, ok := ip.GetServerIdOk(); ok && v != nil {
		id := *v
		info.ServerID = &id
	}
	return info
}

var ipv4Family = ipFamily{
	name: "ipv4", label: "IPv4",
	create: func(ctx context.Context, c *apiclient.Client) (*ipInfo, *http.Response, error) {
		ip, r, err := c.Cloud().CloudIpv4Create(ctx).Execute()
		return fromV4(ip), r, err
	},
	get: func(ctx context.Context, c *apiclient.Client, id int32) (*ipInfo, *http.Response, error) {
		ip, r, err := c.Cloud().CloudIpv4Retrieve(ctx, id).Execute()
		return fromV4(ip), r, err
	},
	destroy: func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error) {
		return c.Cloud().CloudIpv4Destroy(ctx, id).Execute()
	},
	detach: func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error) {
		_, r, err := c.Cloud().CloudIpv4DetachCreate(ctx, id).Execute()
		return r, err
	},
	getRDNS: func(ctx context.Context, c *apiclient.Client, id int32) (string, *http.Response, error) {
		v, r, err := c.Cloud().CloudIpv4RdnsRetrieve(ctx, id).Execute()
		if err != nil {
			return "", r, err
		}
		return v.GetReverseDns(), r, nil
	},
	setRDNS: func(ctx context.Context, c *apiclient.Client, id int32, name string) (*http.Response, error) {
		_, r, err := c.Cloud().CloudIpv4RdnsCreate(ctx, id).ReverseDNSRequest(*sdk.NewReverseDNSRequest(name)).Execute()
		return r, err
	},
	attach: func(ctx context.Context, c *apiclient.Client, serverID int32, ref string, reboot bool) (bool, *http.Response, error) {
		body := sdk.NewAttachIPv4Request(ref)
		body.SetReboot(reboot)
		res, r, err := c.Cloud().CloudServersAttachIpv4Create(ctx, serverID).AttachIPv4Request(*body).Execute()
		if err != nil {
			return false, r, err
		}
		return res.GetRebootRequired() && !res.GetRebooted(), r, nil
	},
}

var ipv6Family = ipFamily{
	name: "ipv6", label: "IPv6",
	create: func(ctx context.Context, c *apiclient.Client) (*ipInfo, *http.Response, error) {
		ip, r, err := c.Cloud().CloudIpv6Create(ctx).Execute()
		return fromV6(ip), r, err
	},
	get: func(ctx context.Context, c *apiclient.Client, id int32) (*ipInfo, *http.Response, error) {
		ip, r, err := c.Cloud().CloudIpv6Retrieve(ctx, id).Execute()
		return fromV6(ip), r, err
	},
	destroy: func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error) {
		return c.Cloud().CloudIpv6Destroy(ctx, id).Execute()
	},
	detach: func(ctx context.Context, c *apiclient.Client, id int32) (*http.Response, error) {
		_, r, err := c.Cloud().CloudIpv6DetachCreate(ctx, id).Execute()
		return r, err
	},
	getRDNS: func(ctx context.Context, c *apiclient.Client, id int32) (string, *http.Response, error) {
		v, r, err := c.Cloud().CloudIpv6RdnsRetrieve(ctx, id).Execute()
		if err != nil {
			return "", r, err
		}
		return v.GetReverseDns(), r, nil
	},
	setRDNS: func(ctx context.Context, c *apiclient.Client, id int32, name string) (*http.Response, error) {
		_, r, err := c.Cloud().CloudIpv6RdnsCreate(ctx, id).ReverseDNSRequest(*sdk.NewReverseDNSRequest(name)).Execute()
		return r, err
	},
	attach: func(ctx context.Context, c *apiclient.Client, serverID int32, ref string, reboot bool) (bool, *http.Response, error) {
		body := sdk.NewAttachIPv6Request(ref)
		body.SetReboot(reboot)
		res, r, err := c.Cloud().CloudServersAttachIpv6Create(ctx, serverID).AttachIPv6Request(*body).Execute()
		if err != nil {
			return false, r, err
		}
		return res.GetRebootRequired() && !res.GetRebooted(), r, nil
	},
}
