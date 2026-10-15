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

func privateNetworkConfig(cidr, gateway string) string {
	gw := ""
	if gateway != "" {
		gw = fmt.Sprintf("gateway = %q", gateway)
	}
	return fmt.Sprintf(`
resource "pidginhost_private_network" "test" {
  address = %q
  %s
}
`, cidr, gw)
}

func TestUnitPrivateNetwork_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + privateNetworkConfig("10.20.0.0/24", "10.20.0.1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_private_network.test", "address", "10.20.0.0/24"),
					resource.TestCheckResourceAttr("pidginhost_private_network.test", "gateway", "10.20.0.1"),
					resource.TestCheckResourceAttr("pidginhost_private_network.test", "slug", "10.20.0.0/24"),
				),
			},
			{
				// Changing the gateway updates in place.
				Config: f.ProviderConfig() + privateNetworkConfig("10.20.0.0/24", "10.20.0.254"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_private_network.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_private_network.test", "gateway", "10.20.0.254"),
					resource.TestCheckResourceAttr("pidginhost_private_network.test", "provisioned", "true"),
				),
			},
			{
				// Removing the gateway clears it (explicit null) in place.
				Config: f.ProviderConfig() + privateNetworkConfig("10.20.0.0/24", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_private_network.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckNoResourceAttr("pidginhost_private_network.test", "gateway"),
			},
			{
				ResourceName:            "pidginhost_private_network.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				PreConfig: func() { f.SetPrivateNetworkGateway(f.PrivateNetworkIDs()[0], "10.20.0.9") },
				Config:    f.ProviderConfig() + privateNetworkConfig("10.20.0.0/24", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_private_network.test", plancheck.ResourceActionUpdate)},
				},
			},
			{
				// Changing the range replaces the network.
				Config: f.ProviderConfig() + privateNetworkConfig("10.30.0.0/24", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_private_network.test", plancheck.ResourceActionDestroyBeforeCreate)},
				},
			},
		},
	})
}

func TestUnitPrivateNetwork_validation(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config:      privateNetworkConfig("10.20.0.5/24", ""),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`has host bits set`),
		}},
	})
}

// testAccCIDR picks a random /24 in 10.213.0.0/16, a range reserved for
// acceptance tests by convention.
func testAccCIDR() string {
	return fmt.Sprintf("10.213.%d.0/24", acctestRandInt()%250+1)
}

func TestAccPrivateNetwork_basic(t *testing.T) {
	cidr := testAccCIDR()
	gw1 := cidr[:len(cidr)-4] + "1"
	gw2 := cidr[:len(cidr)-4] + "254"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: privateNetworkConfig(cidr, gw1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_private_network.test", "address", cidr),
					resource.TestCheckResourceAttrSet("pidginhost_private_network.test", "slug"),
				),
			},
			{
				Config: privateNetworkConfig(cidr, gw2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_private_network.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_private_network.test", "gateway", gw2),
			},
			{
				ResourceName:            "pidginhost_private_network.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "provisioned"},
			},
		},
	})
}
