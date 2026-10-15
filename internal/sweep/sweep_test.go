// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package sweep deletes resources left behind by failed acceptance tests.
//
//	PIDGINHOST_TOKEN=... go test ./internal/sweep -sweep=all -v
//
// (or `make sweep`). A sweeper only ever deletes a resource whose
// identifying name starts with acctest.NamePrefix ("tfacc-"): the hostname
// of a server, the alias of a volume or SSH key, the name of a bucket or
// firewall rule set, the label of a floating IP, or the reverse DNS name of
// a public IP. Resources the API gives no name to (private networks, and
// public IPs without a tfacc- reverse DNS name) are only reported.
package sweep

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
)

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

// pollInterval is how often deletions are polled. Tests lower it.
var pollInterval = 10 * time.Second

func isTest(name string) bool { return strings.HasPrefix(name, acctest.NamePrefix) }

func client() (*apiclient.Client, context.Context, context.CancelFunc, error) {
	c, err := acctest.Client()
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	return c, ctx, cancel, nil
}

func apiErr(what string, err error, resp *http.Response) error {
	return fmt.Errorf("%s: %s", what, apiclient.ErrorDetail(err, resp))
}

func init() {
	resource.AddTestSweepers("pidginhost_server", &resource.Sweeper{Name: "pidginhost_server", F: sweepServers})
	resource.AddTestSweepers("pidginhost_volume", &resource.Sweeper{
		Name: "pidginhost_volume", F: sweepVolumes, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_firewall_rules_set", &resource.Sweeper{
		Name: "pidginhost_firewall_rules_set", F: sweepRuleSets, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_ipv4", &resource.Sweeper{
		Name: "pidginhost_ipv4", F: func(string) error { return sweepIPs(false) }, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_ipv6", &resource.Sweeper{
		Name: "pidginhost_ipv6", F: func(string) error { return sweepIPs(true) }, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_floating_ipv4", &resource.Sweeper{
		Name: "pidginhost_floating_ipv4", F: func(string) error { return sweepFloating(false) }, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_floating_ipv6", &resource.Sweeper{
		Name: "pidginhost_floating_ipv6", F: func(string) error { return sweepFloating(true) }, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_bucket", &resource.Sweeper{Name: "pidginhost_bucket", F: sweepBuckets})
	resource.AddTestSweepers("pidginhost_ssh_key", &resource.Sweeper{
		Name: "pidginhost_ssh_key", F: sweepSSHKeys, Dependencies: []string{"pidginhost_server"},
	})
	resource.AddTestSweepers("pidginhost_private_network", &resource.Sweeper{
		Name: "pidginhost_private_network", F: reportPrivateNetworks, Dependencies: []string{"pidginhost_server"},
	})
}

func sweepServers(string) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	servers, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.Server, bool, *http.Response, error) {
		res, resp, err := c.Cloud().CloudServersList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, resp, err
		}
		return res.GetResults(), res.GetNext() != "", resp, nil
	})
	if err != nil {
		return err
	}
	var deleted []int32
	addresses := map[string]bool{}
	for _, s := range servers {
		if !isTest(s.GetHostname()) || apiclient.IsGoneStatus(string(s.GetStatus())) {
			continue
		}
		// Remember the server's public addresses: an address allocated with
		// new_ipv4/new_ipv6 has no name of its own and may outlive the server.
		nets := s.GetNetworks()
		pub := nets.GetPublic()
		for _, iface := range pub.GetInterfaces() {
			addresses[iface.GetIpv4()] = true
			addresses[iface.GetIpv6()] = true
		}
		addresses[pub.GetIpv4()] = true
		addresses[pub.GetIpv6()] = true
		delete(addresses, "")
		log.Printf("[INFO] deleting server %d (%s)", s.GetId(), s.GetHostname())
		if s.GetDestroyProtection() {
			if _, resp, err := c.Cloud().CloudServersDestroyProtectionCreate(ctx, s.GetId()).
				DestroyProtectionRequest(*sdk.NewDestroyProtectionRequest(false)).Execute(); err != nil {
				return apiErr("disabling destroy protection", err, resp)
			}
		}
		if resp, err := c.Cloud().CloudServersDestroy(ctx, s.GetId()).Execute(); err != nil && !apiclient.IsNotFound(resp) {
			return apiErr(fmt.Sprintf("deleting server %d", s.GetId()), err, resp)
		}
		deleted = append(deleted, s.GetId())
	}
	// Wait for the deletions so that dependent sweepers see freed resources.
	for _, id := range deleted {
		err := apiclient.WaitFor(ctx, pollInterval, fmt.Sprintf("server %d to be deleted", id), func(ctx context.Context) (bool, string, error) {
			s, resp, err := c.Cloud().CloudServersRetrieve(ctx, id).Execute()
			if apiclient.IsNotFound(resp) {
				return true, "", nil
			}
			if err != nil {
				return false, "", apiErr("reading server", err, resp)
			}
			return apiclient.IsGoneStatus(string(s.GetStatus())), string(s.GetStatus()), nil
		})
		if err != nil {
			return err
		}
	}
	return releaseAddresses(ctx, c, addresses)
}

// releaseAddresses releases the unattached public IPs among addresses.
func releaseAddresses(ctx context.Context, c *apiclient.Client, addresses map[string]bool) error {
	if len(addresses) == 0 {
		return nil
	}
	v4, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.PublicIPv4, bool, *http.Response, error) {
		res, resp, err := c.Cloud().CloudIpv4List(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, resp, err
		}
		return res.GetResults(), res.GetNext() != "", resp, nil
	})
	if err != nil {
		return err
	}
	for _, ip := range v4 {
		if addresses[ip.GetAddress()] && !ip.GetAttached() {
			log.Printf("[INFO] releasing IPv4 %d (%s) of a deleted test server", ip.GetId(), ip.GetAddress())
			if resp, err := c.Cloud().CloudIpv4Destroy(ctx, ip.GetId()).Execute(); err != nil && !apiclient.IsNotFound(resp) {
				return apiErr("releasing IPv4", err, resp)
			}
		}
	}
	v6, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.PublicIPv6, bool, *http.Response, error) {
		res, resp, err := c.Cloud().CloudIpv6List(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, resp, err
		}
		return res.GetResults(), res.GetNext() != "", resp, nil
	})
	if err != nil {
		return err
	}
	for _, ip := range v6 {
		if addresses[ip.GetAddress()] && !ip.GetAttached() {
			log.Printf("[INFO] releasing IPv6 %d (%s) of a deleted test server", ip.GetId(), ip.GetAddress())
			if resp, err := c.Cloud().CloudIpv6Destroy(ctx, ip.GetId()).Execute(); err != nil && !apiclient.IsNotFound(resp) {
				return apiErr("releasing IPv6", err, resp)
			}
		}
	}
	return nil
}

func sweepVolumes(string) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	vols, resp, err := c.Cloud().CloudVolumesList(ctx).Execute()
	if err != nil {
		return apiErr("listing volumes", err, resp)
	}
	for _, v := range vols {
		if !isTest(v.GetAlias()) {
			continue
		}
		log.Printf("[INFO] deleting volume %d (%s)", v.GetId(), v.GetAlias())
		if v.GetAttached() {
			if _, resp, err := c.Cloud().CloudVolumesDetachCreate(ctx, v.GetId()).Execute(); err != nil && !apiclient.IsNotFound(resp) {
				return apiErr("detaching volume", err, resp)
			}
		}
		if resp, err := c.Cloud().CloudVolumesDestroy(ctx, v.GetId()).Execute(); err != nil && !apiclient.IsNotFound(resp) {
			return apiErr("deleting volume", err, resp)
		}
	}
	return nil
}

func sweepRuleSets(string) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	sets, resp, err := c.Cloud().CloudFirewallRulesSetList(ctx).Execute()
	if err != nil {
		return apiErr("listing firewall rule sets", err, resp)
	}
	for _, s := range sets {
		if !isTest(s.GetName()) {
			continue
		}
		log.Printf("[INFO] deleting firewall rule set %d (%s)", s.GetId(), s.GetName())
		if resp, err := c.Cloud().CloudFirewallRulesSetDestroy(ctx, s.GetId()).Execute(); err != nil && !apiclient.IsNotFound(resp) {
			return apiErr("deleting firewall rule set", err, resp)
		}
	}
	return nil
}

// sweepIPs releases public IPs whose reverse DNS name starts with tfacc-
// (the acceptance tests always set one). Other unattached IPs are reported.
func sweepIPs(v6 bool) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	type ip struct {
		id       int32
		address  string
		attached bool
	}
	var ips []ip
	if v6 {
		all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.PublicIPv6, bool, *http.Response, error) {
			res, resp, err := c.Cloud().CloudIpv6List(ctx).Page(page).Execute()
			if err != nil {
				return nil, false, resp, err
			}
			return res.GetResults(), res.GetNext() != "", resp, nil
		})
		if err != nil {
			return err
		}
		for _, a := range all {
			ips = append(ips, ip{a.GetId(), a.GetAddress(), a.GetAttached()})
		}
	} else {
		all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.PublicIPv4, bool, *http.Response, error) {
			res, resp, err := c.Cloud().CloudIpv4List(ctx).Page(page).Execute()
			if err != nil {
				return nil, false, resp, err
			}
			return res.GetResults(), res.GetNext() != "", resp, nil
		})
		if err != nil {
			return err
		}
		for _, a := range all {
			ips = append(ips, ip{a.GetId(), a.GetAddress(), a.GetAttached()})
		}
	}

	for _, a := range ips {
		var rdns string
		var resp *http.Response
		if v6 {
			var r *sdk.ReverseDNS
			r, resp, err = c.Cloud().CloudIpv6RdnsRetrieve(ctx, a.id).Execute()
			rdns = r.GetReverseDns()
		} else {
			var r *sdk.ReverseDNS
			r, resp, err = c.Cloud().CloudIpv4RdnsRetrieve(ctx, a.id).Execute()
			rdns = r.GetReverseDns()
		}
		if err != nil {
			return apiErr("reading reverse DNS", err, resp)
		}
		if !isTest(rdns) {
			if !a.attached {
				log.Printf("[WARN] unattached IP %d (%s) has no tfacc- reverse DNS name; not deleting it", a.id, a.address)
			}
			continue
		}
		log.Printf("[INFO] releasing IP %d (%s, %s)", a.id, a.address, rdns)
		if v6 {
			if a.attached {
				_, _, _ = c.Cloud().CloudIpv6DetachCreate(ctx, a.id).Execute()
			}
			resp, err = c.Cloud().CloudIpv6Destroy(ctx, a.id).Execute()
		} else {
			if a.attached {
				_, _, _ = c.Cloud().CloudIpv4DetachCreate(ctx, a.id).Execute()
			}
			resp, err = c.Cloud().CloudIpv4Destroy(ctx, a.id).Execute()
		}
		if err != nil && !apiclient.IsNotFound(resp) {
			return apiErr("releasing IP", err, resp)
		}
	}
	return nil
}

func sweepFloating(v6 bool) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	type fip struct {
		id    int32
		label string
	}
	var fips []fip
	if v6 {
		all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.FloatingIPv6, bool, *http.Response, error) {
			res, resp, err := c.Cloud().CloudFloatingIpv6List(ctx).Page(page).Execute()
			if err != nil {
				return nil, false, resp, err
			}
			return res.GetResults(), res.GetNext() != "", resp, nil
		})
		if err != nil {
			return err
		}
		for _, f := range all {
			fips = append(fips, fip{f.GetId(), f.GetLabel()})
		}
	} else {
		all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.FloatingIPv4, bool, *http.Response, error) {
			res, resp, err := c.Cloud().CloudFloatingIpv4List(ctx).Page(page).Execute()
			if err != nil {
				return nil, false, resp, err
			}
			return res.GetResults(), res.GetNext() != "", resp, nil
		})
		if err != nil {
			return err
		}
		for _, f := range all {
			fips = append(fips, fip{f.GetId(), f.GetLabel()})
		}
	}
	for _, f := range fips {
		if !isTest(f.label) {
			continue
		}
		log.Printf("[INFO] deleting floating IP %d (%s)", f.id, f.label)
		var resp *http.Response
		if v6 {
			resp, err = c.Cloud().CloudFloatingIpv6Destroy(ctx, f.id).Execute()
		} else {
			resp, err = c.Cloud().CloudFloatingIpv4Destroy(ctx, f.id).Execute()
		}
		if err != nil && !apiclient.IsNotFound(resp) {
			return apiErr("deleting floating IP", err, resp)
		}
	}
	return nil
}

func sweepBuckets(string) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	buckets, resp, err := c.ListBuckets(ctx)
	if err != nil {
		return apiErr("listing buckets", err, resp)
	}
	for _, b := range buckets {
		if !isTest(b.Name) || apiclient.IsGoneStatus(b.Status) {
			continue
		}
		log.Printf("[INFO] deleting bucket %d (%s)", b.ID, b.Name)
		if _, resp, err := c.Cloud().CloudBucketsDestroy(ctx, b.ID).Execute(); err != nil && !apiclient.IsNotFound(resp) {
			return apiErr("deleting bucket", err, resp)
		}
	}
	return nil
}

func sweepSSHKeys(string) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	keys, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.SSHKey, bool, *http.Response, error) {
		res, resp, err := c.Account().AccountSshKeysList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, resp, err
		}
		return res.GetResults(), res.GetNext() != "", resp, nil
	})
	if err != nil {
		return err
	}
	for _, k := range keys {
		if !isTest(k.GetAlias()) {
			continue
		}
		log.Printf("[INFO] deleting SSH key %d (%s)", k.GetId(), k.GetAlias())
		if resp, err := c.Account().AccountSshKeysDestroy(ctx, apiclient.FormatID(k.GetId())).Execute(); err != nil && !apiclient.IsNotFound(resp) {
			return apiErr("deleting SSH key", err, resp)
		}
	}
	return nil
}

// reportPrivateNetworks lists networks in the range the acceptance tests use
// (10.213.0.0/16). Private networks have no name, so they cannot carry the
// tfacc- prefix and are never deleted automatically.
func reportPrivateNetworks(string) error {
	c, ctx, cancel, err := client()
	if err != nil {
		return err
	}
	defer cancel()
	nets, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.PrivateNetwork, bool, *http.Response, error) {
		res, resp, err := c.Cloud().CloudPrivateNetworksList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, resp, err
		}
		return res.GetResults(), res.GetNext() != "", resp, nil
	})
	if err != nil {
		return err
	}
	for _, n := range nets {
		if strings.HasPrefix(n.GetAddress(), "10.213.") {
			log.Printf("[WARN] private network %d (%s) may be left over from an acceptance test; "+
				"private networks have no name, so it is not deleted automatically", n.GetId(), n.GetAddress())
		}
	}
	return nil
}
