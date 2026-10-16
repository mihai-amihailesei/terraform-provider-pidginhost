// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	sdk "github.com/pidginhost/sdk-go"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
)

// waitForActive polls the server until its status is "active".
//
// The spec describes server creation as asynchronous (POST answers with only
// {id}) and lists the statuses in ResourceStatusEnum, but not which
// transitions happen. This treats pending, provisioning, upgrading and
// resuming as transitional, and every status that cannot lead to "active"
// (failed, suspended, cancelling, cancelled, terminating, terminated) as a
// failure.
func waitForActive(ctx context.Context, c *apiclient.Client, id int32, action string) (*sdk.ServerDetail, error) {
	var last *sdk.ServerDetail
	err := apiclient.WaitFor(ctx, c.PollInterval, fmt.Sprintf("server %d to become active after %s", id, action),
		func(ctx context.Context) (bool, string, error) {
			s, httpResp, err := c.Cloud().CloudServersRetrieve(ctx, id).Execute()
			if err != nil {
				return false, "", fmt.Errorf("reading server %d: %s", id, apiclient.ErrorDetail(err, httpResp))
			}
			last = s
			status := string(s.GetStatus())
			switch {
			case status == apiclient.StatusActive:
				return true, status, nil
			case apiclient.IsTerminalFailure(status):
				return false, status, fmt.Errorf("%w: server %d reached status %q during %s; check the server in the "+
					"Pidgin Host control panel (the retry-provision action may help for provisioning failures)",
					apiclient.ErrWaitFailed, id, status, action)
			default:
				return false, status, nil
			}
		})
	return last, err
}

// waitForGone polls until the server returns 404 or a terminated status.
func waitForGone(ctx context.Context, c *apiclient.Client, id int32) error {
	return apiclient.WaitFor(ctx, c.PollInterval, fmt.Sprintf("server %d to be deleted", id),
		func(ctx context.Context) (bool, string, error) {
			s, httpResp, err := c.Cloud().CloudServersRetrieve(ctx, id).Execute()
			if apiclient.IsNotFound(httpResp) {
				return true, "", nil
			}
			if err != nil {
				return false, "", fmt.Errorf("reading server %d: %s", id, apiclient.ErrorDetail(err, httpResp))
			}
			status := string(s.GetStatus())
			return apiclient.IsGoneStatus(status), status, nil
		})
}

// normalizePower maps the free-form power status of the API onto
// running/stopped.
//
// Assumption: PowerManagement.status is a plain string in the spec, with no
// enum. The official provider treats "running" as powered on; the other
// values below are the usual Proxmox/libvirt names for a powered-off guest.
// Any other value is passed through unchanged so it is visible in state.
func normalizePower(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running", "on", "started":
		return powerRunning
	case "stopped", "off", "shutoff", "shut off", "shutdown", "powered off":
		return powerStopped
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// powerState returns the normalized power state of a server. It prefers the
// power-management endpoint and falls back to the "status" key of the
// free-form ServerDetail.machine object.
func powerState(ctx context.Context, c *apiclient.Client, id int32, detail *sdk.ServerDetail) (string, error) {
	pm, httpResp, err := c.Cloud().CloudServersPowerManagementRetrieve(ctx, id).Execute()
	if err == nil {
		return normalizePower(pm.GetStatus()), nil
	}
	if detail != nil {
		if s, ok := detail.GetMachine()["status"].(string); ok && s != "" {
			return normalizePower(s), nil
		}
	}
	return "", fmt.Errorf("reading power state of server %d: %s", id, apiclient.ErrorDetail(err, httpResp))
}

func powerAction(ctx context.Context, c *apiclient.Client, id int32, action sdk.PowerManagementRequestActionEnum) error {
	_, httpResp, err := c.Cloud().CloudServersPowerManagementCreate(ctx, id).
		PowerManagementRequest(*sdk.NewPowerManagementRequest(action)).Execute()
	if err != nil {
		return fmt.Errorf("power action %q on server %d: %s", action, id, apiclient.ErrorDetail(err, httpResp))
	}
	return nil
}

func waitForPower(ctx context.Context, c *apiclient.Client, id int32, want string) error {
	return apiclient.WaitFor(ctx, c.PollInterval, fmt.Sprintf("server %d to be %s", id, want),
		func(ctx context.Context) (bool, string, error) {
			got, err := powerState(ctx, c, id, nil)
			if err != nil {
				return false, "", err
			}
			return got == want, got, nil
		})
}

// setPower brings the server to the wanted power state. Stopping first asks
// the guest to shut down and, if it is still running after graceTimeout,
// powers it off.
func setPower(ctx context.Context, c *apiclient.Client, id int32, want string, graceTimeout time.Duration) error {
	current, err := powerState(ctx, c, id, nil)
	if err != nil {
		return err
	}
	if current == want {
		return nil
	}
	if want == powerRunning {
		if err := powerAction(ctx, c, id, sdk.POWERMANAGEMENTREQUESTACTIONENUM_START); err != nil {
			return err
		}
		return waitForPower(ctx, c, id, powerRunning)
	}

	if err := powerAction(ctx, c, id, sdk.POWERMANAGEMENTREQUESTACTIONENUM_SHUTDOWN); err != nil {
		return err
	}
	graceCtx, cancel := context.WithTimeout(ctx, graceTimeout)
	defer cancel()
	if err := waitForPower(graceCtx, c, id, powerStopped); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return err
	}
	tflog.Warn(ctx, "graceful shutdown did not complete; forcing power off", map[string]any{"server_id": id})
	if err := powerAction(ctx, c, id, sdk.POWERMANAGEMENTREQUESTACTIONENUM_STOP); err != nil {
		return err
	}
	return waitForPower(ctx, c, id, powerStopped)
}

// publicInterface returns the firewall settings of the server's public
// interface. found is false when the server has no public interface (the
// endpoint answers 404).
func publicInterface(ctx context.Context, c *apiclient.Client, id int32) (pi *sdk.PublicInterface, found bool, err error) {
	pi, httpResp, err := c.Cloud().CloudServersPublicInterfaceRetrieve(ctx, id).Execute()
	if apiclient.IsNotFound(httpResp) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading public interface of server %d: %s", id, apiclient.ErrorDetail(err, httpResp))
	}
	return pi, true, nil
}

// setFirewall applies a rule set and policies to the public interface.
// When manageSet is false the fw_rules_set field is omitted, which leaves
// whatever set is attached (for example the platform's read-only set on a
// free-tier server) untouched. Otherwise an empty setID detaches the current
// set.
//
// The field must be sent as an explicit JSON null to detach a set: omitting
// it leaves the current set attached. (The official provider's "inconsistent
// result after apply" bug came from fw_rules_set being cleared on one side
// but not the other.)
func setFirewall(ctx context.Context, c *apiclient.Client, id int32, manageSet bool, setID, policyIn, policyOut string) error {
	body := sdk.NewPublicInterfaceRequest()
	switch {
	case !manageSet:
	case setID == "":
		body.SetFwRulesSetNil()
	default:
		body.SetFwRulesSet(setID)
	}
	if policyIn != "" {
		body.SetFwPolicyIn(sdk.FwPolicyOutEnum(policyIn))
	}
	if policyOut != "" {
		body.SetFwPolicyOut(sdk.FwPolicyOutEnum(policyOut))
	}
	_, httpResp, err := c.Cloud().CloudServersPublicInterfaceCreate(ctx, id).PublicInterfaceRequest(*body).Execute()
	if err != nil {
		return fmt.Errorf("updating firewall of server %d: %s", id, apiclient.ErrorDetail(err, httpResp))
	}
	return nil
}

// resolveRulesSetID turns the fw_rules_set value returned by the public
// interface into a rule set ID, so that state holds a stable ID whichever
// form the API uses.
//
// Assumption: the spec documents the field as "ID or slug", but
// FirewallRulesSet has no slug property. A numeric value is taken as the ID.
// The live API reports the read-only set it attaches to free-tier servers by
// its name in lower case ("free tier fw" for the set named "Free tier FW";
// see UPSTREAM_ISSUES C5), so anything else is matched against rule set
// names, read-only sets included, ignoring case. When several sets match,
// prior (the ID currently in state) wins if it is one of them, then an
// exact-case match. If nothing matches, the raw value is returned so that
// the difference shows up as drift rather than being hidden.
func resolveRulesSetID(ctx context.Context, c *apiclient.Client, raw, prior string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if _, err := strconv.ParseInt(raw, 10, 32); err == nil {
		return raw, nil
	}
	sets, httpResp, err := c.Cloud().CloudFirewallRulesSetList(ctx).Execute()
	if err != nil {
		return "", fmt.Errorf("listing firewall rule sets: %s", apiclient.ErrorDetail(err, httpResp))
	}
	return matchRulesSetName(sets, raw, prior), nil
}

func matchRulesSetName(sets []sdk.FirewallRulesSet, raw, prior string) string {
	var matches, exact []string
	for _, s := range sets {
		if !strings.EqualFold(s.GetName(), raw) {
			continue
		}
		id := apiclient.FormatID(s.GetId())
		if id == prior {
			return id
		}
		matches = append(matches, id)
		if s.GetName() == raw {
			exact = append(exact, id)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0]
	case len(exact) == 1:
		return exact[0]
	default:
		return raw
	}
}

// samePackage reports whether two package references (ID, slug or name)
// denote the same package. It only calls the API when the strings differ.
//
// The spec types ServerDetail.package as a plain string without saying
// whether it holds the slug, the ID or the display name, while the create
// and modify-package calls accept "ID or slug". Comparing through the
// catalog keeps a configuration that uses an ID from showing a permanent
// diff (and triggering modify-package on every apply).
func samePackage(ctx context.Context, c *apiclient.Client, a, b string) (bool, error) {
	if strings.EqualFold(a, b) {
		return true, nil
	}
	all, err := apiclient.ListAll(ctx, func(ctx context.Context, page int32) ([]sdk.ServerProduct, bool, *http.Response, error) {
		res, httpResp, err := c.Cloud().CloudServerPackagesList(ctx).Page(page).Execute()
		if err != nil {
			return nil, false, httpResp, err
		}
		return res.GetResults(), res.GetNext() != "", httpResp, nil
	})
	if err != nil {
		return false, err
	}
	find := func(ref string) int32 {
		for _, p := range all {
			if apiclient.FormatID(p.GetId()) == ref || strings.EqualFold(p.GetSlug(), ref) || strings.EqualFold(p.GetName(), ref) {
				return p.GetId()
			}
		}
		return -1
	}
	ia, ib := find(a), find(b)
	return ia != -1 && ia == ib, nil
}

// findServerByHostname returns the ID of the server with this hostname.
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
	var ids []int32
	for _, s := range all {
		if strings.EqualFold(s.GetHostname(), hostname) {
			ids = append(ids, s.GetId())
		}
	}
	switch len(ids) {
	case 0:
		return 0, fmt.Errorf("no server has the hostname %q", hostname)
	case 1:
		return ids[0], nil
	default:
		return 0, fmt.Errorf("%d servers have the hostname %q; look the server up by id instead", len(ids), hostname)
	}
}
