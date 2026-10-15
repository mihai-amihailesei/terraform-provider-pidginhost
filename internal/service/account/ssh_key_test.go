// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package account_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/mihai-amihailesei/pidgin-host/internal/acctest"
)

// testPublicKey is a throwaway public key generated for these tests.
const testPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGi1pA0bZmI3Ys3l4y0b9O3xk3iOe9lXQ6m8T7YkD2xq tfacc@example.invalid"

func sshKeyConfig(alias string) string {
	return fmt.Sprintf(`
resource "pidginhost_ssh_key" "test" {
  alias      = %q
  # The trailing newline must not cause drift.
  public_key = "%s\n"
}
`, alias, testPublicKey)
}

func TestUnitSSHKey_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + sshKeyConfig("first"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_ssh_key.test", "alias", "first"),
					resource.TestMatchResourceAttr("pidginhost_ssh_key.test", "fingerprint", regexp.MustCompile(`^SHA256:`)),
					resource.TestCheckResourceAttrSet("pidginhost_ssh_key.test", "id"),
				),
			},
			{
				// Renaming updates in place.
				Config: f.ProviderConfig() + sshKeyConfig("second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_ssh_key.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_ssh_key.test", "alias", "second"),
			},
			{
				ResourceName:            "pidginhost_ssh_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_key", "timeouts"},
			},
			{
				// Out-of-band rename is detected and reverted.
				PreConfig: func() { f.SetSSHKeyAlias(f.SSHKeyIDs()[0], "changed-elsewhere") },
				Config:    f.ProviderConfig() + sshKeyConfig("second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_ssh_key.test", plancheck.ResourceActionUpdate)},
				},
			},
			{
				// Deleted out of band: removed from state and recreated.
				PreConfig: func() { f.DeleteSSHKey(f.SSHKeyIDs()[0]) },
				Config:    f.ProviderConfig() + sshKeyConfig("second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_ssh_key.test", plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

func TestUnitSSHKey_invalidKey(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config:      f.ProviderConfig() + `resource "pidginhost_ssh_key" "bad" { public_key = "not a key" }`,
			ExpectError: regexp.MustCompile(`key: Invalid SSH key`),
		}},
	})
}

func TestUnitSSHKeyDataSource(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + sshKeyConfig("lookup") + `
data "pidginhost_ssh_key" "by_alias" { alias = pidginhost_ssh_key.test.alias }
data "pidginhost_ssh_key" "by_id" { id = pidginhost_ssh_key.test.id }
data "pidginhost_ssh_key" "by_fp" { fingerprint = pidginhost_ssh_key.test.fingerprint }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.pidginhost_ssh_key.by_alias", "id", "pidginhost_ssh_key.test", "id"),
				resource.TestCheckResourceAttrPair("data.pidginhost_ssh_key.by_id", "alias", "pidginhost_ssh_key.test", "alias"),
				resource.TestCheckResourceAttrPair("data.pidginhost_ssh_key.by_fp", "id", "pidginhost_ssh_key.test", "id"),
				resource.TestCheckResourceAttr("data.pidginhost_ssh_key.by_id", "public_key", testPublicKey),
			),
		}},
	})
}

func TestUnitSSHKeyDataSource_requiresOneSelector(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config:      f.ProviderConfig() + `data "pidginhost_ssh_key" "x" {}`,
			ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured`),
		}},
	})
}

func TestAccSSHKey_basic(t *testing.T) {
	name := acctest.RandomName("key")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: sshKeyConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_ssh_key.test", "alias", name),
					resource.TestCheckResourceAttrSet("pidginhost_ssh_key.test", "fingerprint"),
				),
			},
			{
				Config: sshKeyConfig(name + "-renamed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("pidginhost_ssh_key.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("pidginhost_ssh_key.test", "alias", name+"-renamed"),
			},
			{
				ResourceName:            "pidginhost_ssh_key.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_key", "timeouts"},
			},
		},
	})
}

func TestAccSSHKeyDataSource_basic(t *testing.T) {
	name := acctest.RandomName("key")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: sshKeyConfig(name) + `
data "pidginhost_ssh_key" "by_alias" { alias = pidginhost_ssh_key.test.alias }
`,
			Check: resource.TestCheckResourceAttrPair("data.pidginhost_ssh_key.by_alias", "fingerprint", "pidginhost_ssh_key.test", "fingerprint"),
		}},
	})
}
