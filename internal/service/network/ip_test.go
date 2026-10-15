// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package network_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/mihai-amihailesei/pidgin-host/internal/acctest"
)

func ipConfig(family, rdns string) string {
	r := ""
	if rdns != "" {
		r = fmt.Sprintf("reverse_dns = %q", rdns)
	}
	return fmt.Sprintf(`
resource "pidginhost_%s" "test" {
  %s
}
`, family, r)
}

func testUnitIP(t *testing.T, family string, v6 bool, addr *regexp.Regexp) {
	f := acctest.NewFake(t)
	name := "pidginhost_" + family + ".test"
	ids := f.IPv4IDs
	if v6 {
		ids = f.IPv6IDs
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				// Without reverse_dns, the API default is reported.
				Config: f.ProviderConfig() + ipConfig(family, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(name, "address", addr),
					resource.TestMatchResourceAttr(name, "reverse_dns", regexp.MustCompile(`\.fake\.invalid$`)),
					resource.TestCheckResourceAttr(name, "attached", "false"),
					resource.TestCheckNoResourceAttr(name, "server_id"),
				),
			},
			{
				Config: f.ProviderConfig() + ipConfig(family, "mail.example.com"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "reverse_dns", "mail.example.com"),
			},
			{
				// A trailing dot or different case is not drift.
				PreConfig: func() { f.SetReverseDNS(v6, ids()[0], "MAIL.example.com.") },
				Config:    f.ProviderConfig() + ipConfig(family, "mail.example.com"),
				PlanOnly:  true,
			},
			{
				PreConfig: func() { f.SetReverseDNS(v6, ids()[0], "other.example.com") },
				Config:    f.ProviderConfig() + ipConfig(family, "mail.example.com"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
	if len(ids()) != 0 {
		t.Fatalf("addresses left behind: %v", ids())
	}
}

func TestUnitIPv4_lifecycle(t *testing.T) {
	testUnitIP(t, "ipv4", false, regexp.MustCompile(`^192\.0\.2\.\d+$`))
}

func TestUnitIPv6_lifecycle(t *testing.T) {
	testUnitIP(t, "ipv6", true, regexp.MustCompile(`^2001:db8::`))
}

// rdnsDomain is used for reverse DNS in acceptance tests. Override it with a
// domain you control if the API validates that the name resolves.
func rdnsDomain() string {
	return acctest.Getenv("PIDGINHOST_ACC_RDNS_DOMAIN", "example.com")
}

func testAccIP(t *testing.T, family string) {
	name := "pidginhost_" + family + ".test"
	first := acctest.RandomName("rdns") + "." + rdnsDomain()
	second := acctest.RandomName("rdns") + "." + rdnsDomain()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: ipConfig(family, first),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(name, "address"),
					resource.TestCheckResourceAttr(name, "reverse_dns", first),
				),
			},
			{
				Config: ipConfig(family, second),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr(name, "reverse_dns", second),
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

func TestAccIPv4_basic(t *testing.T) { testAccIP(t, "ipv4") }

func TestAccIPv6_basic(t *testing.T) { testAccIP(t, "ipv6") }
