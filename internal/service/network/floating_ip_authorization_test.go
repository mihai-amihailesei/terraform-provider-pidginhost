// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
)

func authConfig(family, server string) string {
	return fmt.Sprintf(`
resource "pidginhost_floating_%[1]s" "test" {
  label = "vip"
}

resource "pidginhost_floating_ip_authorization" "test" {
  floating_%[1]s_id = pidginhost_floating_%[1]s.test.id
  server_id        = pidginhost_server.%[2]s.id
}
`, family, server)
}

func TestUnitFloatingIPAuthorization_lifecycle(t *testing.T) {
	for _, tc := range []struct {
		family string
		v6     bool
	}{{"ipv4", false}, {"ipv6", true}} {
		t.Run(tc.family, func(t *testing.T) {
			f := acctest.NewFake(t)
			servers := f.ProviderConfig() + acctest.FakeServerConfig("a", "fip-a") + acctest.FakeServerConfig("b", "fip-b")
			const name = "pidginhost_floating_ip_authorization.test"
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: acctest.FakeProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: servers + authConfig(tc.family, "a"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(name, "server_hostname", "fip-a"),
							resource.TestMatchResourceAttr(name, "id", regexp.MustCompile(`^`+tc.family+`/\d+/\d+$`)),
						),
					},
					{
						Config: servers + authConfig(tc.family, "a"),
						Check:  resource.TestCheckResourceAttr("pidginhost_floating_"+tc.family+".test", "authorized_server_count", "1"),
					},
					{
						ResourceName:            name,
						ImportState:             true,
						ImportStateVerify:       true,
						ImportStateVerifyIgnore: []string{"timeouts"},
					},
					{
						Config: servers + authConfig(tc.family, "b"),
						ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
							plancheck.ExpectResourceAction("pidginhost_floating_"+tc.family+".test", plancheck.ResourceActionNoop),
						}},
						Check: resource.TestCheckResourceAttr(name, "server_hostname", "fip-b"),
					},
					{
						PreConfig: func() { f.Unauthorize(tc.v6, f.FloatingIPIDs(tc.v6)[0]) },
						Config:    servers + authConfig(tc.family, "b"),
						ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(name, plancheck.ResourceActionCreate),
						}},
					},
				},
			})
		})
	}
}

func TestUnitFloatingIPAuthorization_exactlyOne(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
resource "pidginhost_floating_ip_authorization" "test" {
  floating_ipv4_id = "1"
  floating_ipv6_id = "2"
  server_id        = "3"
}
`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
		}},
	})
}

func TestAccFloatingIPAuthorization_basic(t *testing.T) {
	cfg := acctest.CatalogConfig() + acctest.AccServerConfig("a", acctest.RandomName("fip")) +
		fmt.Sprintf(`
resource "pidginhost_floating_ipv4" "test" {
  label = %q
}

resource "pidginhost_floating_ip_authorization" "test" {
  floating_ipv4_id = pidginhost_floating_ipv4.test.id
  server_id        = pidginhost_server.a.id
}
`, acctest.RandomName("fip"))
	const name = "pidginhost_floating_ip_authorization.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check:  resource.TestCheckResourceAttrPair(name, "server_hostname", "pidginhost_server.a", "hostname"),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
