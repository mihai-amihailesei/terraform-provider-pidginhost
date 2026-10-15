// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
)

func floatingConfig(family, label, rdns string) string {
	r := ""
	if rdns != "" {
		r = fmt.Sprintf("reverse_dns = %q", rdns)
	}
	return fmt.Sprintf(`
resource "pidginhost_floating_%s" "test" {
  label = %q
  %s
}
`, family, label, r)
}

func testUnitFloating(t *testing.T, family string) {
	f := acctest.NewFake(t)
	name := "pidginhost_floating_" + family + ".test"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + floatingConfig(family, "vip", "vip.example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "label", "vip"),
					resource.TestCheckResourceAttr(name, "reverse_dns", "vip.example.com"),
					resource.TestCheckResourceAttr(name, "authorized_server_count", "0"),
					resource.TestCheckResourceAttrSet(name, "address"),
					resource.TestCheckResourceAttrSet(name, "created_at"),
				),
			},
			{
				Config: f.ProviderConfig() + floatingConfig(family, "vip", "vip2.example.com"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "reverse_dns", "vip2.example.com"),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				// The API cannot relabel: replacement.
				Config: f.ProviderConfig() + floatingConfig(family, "vip-renamed", "vip2.example.com"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

func TestUnitFloatingIPv4_lifecycle(t *testing.T) { testUnitFloating(t, "ipv4") }

func TestUnitFloatingIPv6_lifecycle(t *testing.T) { testUnitFloating(t, "ipv6") }

func testAccFloating(t *testing.T, family string) {
	name := "pidginhost_floating_" + family + ".test"
	label := acctest.RandomName("fip")
	first := acctest.RandomName("rdns") + "." + rdnsDomain()
	second := acctest.RandomName("rdns") + "." + rdnsDomain()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: floatingConfig(family, label, first),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "label", label),
					resource.TestCheckResourceAttr(name, "reverse_dns", first),
				),
			},
			{
				Config: floatingConfig(family, label, second),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "reverse_dns", second),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "authorized_server_count"},
			},
		},
	})
}

func TestAccFloatingIPv4_basic(t *testing.T) { testAccFloating(t, "ipv4") }

func TestAccFloatingIPv6_basic(t *testing.T) { testAccFloating(t, "ipv6") }
