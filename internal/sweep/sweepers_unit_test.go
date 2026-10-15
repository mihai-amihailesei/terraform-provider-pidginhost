// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package sweep

import (
	"context"
	"testing"
	"time"

	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/fakeapi"
)

// TestUnitSweepersOnlyDeleteTestResources runs every sweeper against the fake
// API and checks that resources without the tfacc- prefix survive.
func TestUnitSweepersOnlyDeleteTestResources(t *testing.T) {
	f := fakeapi.New(t)
	t.Setenv("PIDGINHOST_TOKEN", fakeapi.Token)
	t.Setenv("PIDGINHOST_API_URL", f.URL)
	pollInterval = 10 * time.Millisecond

	c, err := acctest.Client()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cloud := c.Cloud()

	keep, sweep := "prod-", acctest.NamePrefix
	var keepServer, sweepServer int32
	for _, prefix := range []string{keep, sweep} {
		srv := f.CreateServer(prefix+"server", "debian-12", "cloud-xs")
		if prefix == keep {
			keepServer = srv
		} else {
			sweepServer = srv
		}
		if _, _, err := cloud.CloudServersDestroyProtectionCreate(ctx, srv).DestroyProtectionRequest(*sdk.NewDestroyProtectionRequest(true)).Execute(); err != nil {
			t.Fatal(err)
		}
		vol := sdk.NewVolumeRequest(10, "ssd")
		vol.SetAlias(prefix + "vol")
		if _, _, err := cloud.CloudServersVolumesCreate(ctx, apiclient.FormatID(srv)).VolumeRequest(*vol).Execute(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := cloud.CloudFirewallRulesSetCreate(ctx).FirewallRulesSetRequest(*sdk.NewFirewallRulesSetRequest(prefix + "fw")).Execute(); err != nil {
			t.Fatal(err)
		}
		ip, _, err := cloud.CloudIpv4Create(ctx).Execute()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := cloud.CloudIpv4RdnsCreate(ctx, ip.GetId()).ReverseDNSRequest(*sdk.NewReverseDNSRequest(prefix + "ip.example.com")).Execute(); err != nil {
			t.Fatal(err)
		}
		fipReq := sdk.NewFloatingIPv4CreateRequest()
		fipReq.SetLabel(prefix + "fip")
		if _, _, err := cloud.CloudFloatingIpv4Create(ctx).FloatingIPv4CreateRequest(*fipReq).Execute(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.CreateBucket(ctx, prefix+"bucket", 1, false); err != nil {
			t.Fatal(err)
		}
		key := sdk.NewSSHKeyRequest("ssh-ed25519 AAAA" + prefix)
		key.SetAlias(prefix + "key")
		if _, _, err := c.Account().AccountSshKeysCreate(ctx).SSHKeyRequest(*key).Execute(); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := cloud.CloudPrivateNetworksCreate(ctx).PrivateNetworkRequest(*sdk.NewPrivateNetworkRequest("10.213.7.0/24")).Execute(); err != nil {
		t.Fatal(err)
	}

	for _, sw := range []func(string) error{
		sweepServers, sweepVolumes, sweepRuleSets,
		func(string) error { return sweepIPs(false) },
		func(string) error { return sweepFloating(false) },
		sweepBuckets, sweepSSHKeys, reportPrivateNetworks,
	} {
		if err := sw(""); err != nil {
			t.Fatal(err)
		}
	}

	if ids := f.ServerIDs(); len(ids) != 1 || ids[0] != keepServer {
		t.Errorf("servers after sweep = %v, want only %d (not %d)", ids, keepServer, sweepServer)
	}
	if n := len(f.VolumeIDs()); n != 1 {
		t.Errorf("%d volumes left, want 1", n)
	}
	if n := len(f.RuleSetIDs()); n != 1 {
		t.Errorf("%d rule sets left, want 1", n)
	}
	if n := len(f.FloatingIPIDs(false)); n != 1 {
		t.Errorf("%d floating IPs left, want 1", n)
	}
	if n := len(f.SSHKeyIDs()); n != 1 {
		t.Errorf("%d SSH keys left, want 1", n)
	}
	if n := len(f.PrivateNetworkIDs()); n != 1 {
		t.Errorf("private network was deleted; it must only be reported")
	}
	// Public IPs: the kept server's own IP and the prod- IP remain; the
	// deleted test server's IP and the tfacc- IP are released.
	if n := len(f.IPv4IDs()); n != 2 {
		t.Errorf("%d IPv4s left, want 2 (the prod- server's IP and the prod- IP)", n)
	}
	// Bucket deletion is asynchronous; it is marked for deletion.
	buckets, _, err := c.ListBuckets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range buckets {
		deleting := b.Status == "cancelling"
		if isTest(b.Name) != deleting {
			t.Errorf("bucket %s has status %s", b.Name, b.Status)
		}
	}
}
