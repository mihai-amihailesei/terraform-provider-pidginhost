// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package firewall_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
)

func rulesSetConfig(name string) string {
	return fmt.Sprintf(`
resource "pidginhost_firewall_rules_set" "test" {
  name = %q
}
`, name)
}

func TestUnitFirewallRulesSet_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + rulesSetConfig("web"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_firewall_rules_set.test", "name", "web"),
					resource.TestCheckResourceAttr("pidginhost_firewall_rules_set.test", "status", "validated"),
					resource.TestCheckResourceAttr("pidginhost_firewall_rules_set.test", "read_only", "false"),
				),
			},
			{
				Config: f.ProviderConfig() + rulesSetConfig("web-renamed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_firewall_rules_set.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_firewall_rules_set.test", "name", "web-renamed"),
			},
			{
				ResourceName:            "pidginhost_firewall_rules_set.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				PreConfig: func() { f.RenameRuleSet(f.RuleSetIDs()[0], "drifted") },
				Config:    f.ProviderConfig() + rulesSetConfig("web-renamed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_firewall_rules_set.test", plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

func TestAccFirewallRulesSet_basic(t *testing.T) {
	name := acctest.RandomName("fw")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: rulesSetConfig(name),
				Check:  resource.TestCheckResourceAttr("pidginhost_firewall_rules_set.test", "name", name),
			},
			{
				Config: rulesSetConfig(name + "-renamed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_firewall_rules_set.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_firewall_rules_set.test", "name", name+"-renamed"),
			},
			{
				ResourceName:            "pidginhost_firewall_rules_set.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "status"},
			},
		},
	})
}
