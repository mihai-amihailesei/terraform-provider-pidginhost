// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package firewall_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mihai-amihailesei/pidgin-host/internal/acctest"
)

func rulesConfig(name, sshPort string, withHTTP bool) string {
	cfg := rulesSetConfig(name) + fmt.Sprintf(`
resource "pidginhost_firewall_rule" "ssh" {
  rules_set_id     = pidginhost_firewall_rules_set.test.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  destination_port = %q
  source           = "192.0.2.0/24"
}
`, sshPort)
	if withHTTP {
		cfg += `
resource "pidginhost_firewall_rule" "http" {
  rules_set_id     = pidginhost_firewall_rules_set.test.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  destination_port = "80,443"
}
`
	}
	return cfg
}

func importID(name string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		set, err := acctest.AttrFromState(s, name, "rules_set_id")
		if err != nil {
			return "", err
		}
		id, err := acctest.AttrFromState(s, name, "id")
		return set + "/" + id, err
	}
}

func TestUnitFirewallRule_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + rulesConfig("web", "22", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "destination_port", "22"),
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "source_port", ""),
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "enabled", "true"),
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "has_error", "false"),
					resource.TestCheckResourceAttrSet("pidginhost_firewall_rule.ssh", "position"),
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.http", "destination_port", "80,443"),
				),
			},
			{
				// Changing a rule and removing another updates in place and
				// never touches the set.
				Config: f.ProviderConfig() + rulesConfig("web", "2222", false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("pidginhost_firewall_rule.ssh", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("pidginhost_firewall_rule.http", plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction("pidginhost_firewall_rules_set.test", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "destination_port", "2222"),
			},
			{
				ResourceName:            "pidginhost_firewall_rule.ssh",
				ImportState:             true,
				ImportStateIdFunc:       importID("pidginhost_firewall_rule.ssh"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				// Out-of-band change is detected and reverted in place.
				PreConfig: func() { f.SetRuleDport(f.RuleSetIDs()[0], "9999") },
				Config:    f.ProviderConfig() + rulesConfig("web", "2222", false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_firewall_rule.ssh", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "destination_port", "2222"),
			},
			{
				// Removing an optional field from the config clears it.
				Config: f.ProviderConfig() + rulesSetConfig("web") + `
resource "pidginhost_firewall_rule" "ssh" {
  rules_set_id     = pidginhost_firewall_rules_set.test.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  destination_port = "2222"
}
`,
				Check: resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "source", ""),
			},
		},
	})
}

func TestUnitFirewallRule_invalidRuleFails(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + rulesSetConfig("web") + `
resource "pidginhost_firewall_rule" "bad" {
  rules_set_id     = pidginhost_firewall_rules_set.test.id
  direction        = "in"
  action           = "DROP"
  destination_port = "22"
}
`,
			ExpectError: regexp.MustCompile(`(?s)flagged it as invalid: Ports can only be used\s+together with a protocol`),
		}},
	})
}

func TestUnitFirewallRule_validation(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
resource "pidginhost_firewall_rule" "bad" {
  rules_set_id = "1"
  direction    = "sideways"
  action       = "ACCEPT"
}
`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`value must be one of`),
		}},
	})
}

func TestUnitFirewallRule_badImportID(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + `
resource "pidginhost_firewall_rule" "missing" {
  rules_set_id = "1"
  direction    = "in"
  action       = "ACCEPT"
}
`,
			ResourceName:  "pidginhost_firewall_rule.missing",
			ImportState:   true,
			ImportStateId: "12",
			ExpectError:   regexp.MustCompile(`expected the format <rules_set_id>/<rule_id>`),
		}},
	})
}

func TestAccFirewallRule_basic(t *testing.T) {
	name := acctest.RandomName("fw")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: rulesConfig(name, "22", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "destination_port", "22"),
					resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "has_error", "false"),
				),
			},
			{
				Config: rulesConfig(name, "2222", false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("pidginhost_firewall_rule.ssh", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("pidginhost_firewall_rules_set.test", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_firewall_rule.ssh", "destination_port", "2222"),
			},
			{
				ResourceName:            "pidginhost_firewall_rule.ssh",
				ImportState:             true,
				ImportStateIdFunc:       importID("pidginhost_firewall_rule.ssh"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
