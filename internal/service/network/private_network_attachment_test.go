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

func attachmentConfig(address string) string {
	addr := ""
	if address != "" {
		addr = fmt.Sprintf("address = %q", address)
	}
	return fmt.Sprintf(`
resource "pidginhost_private_network_attachment" "test" {
  private_network_id = pidginhost_private_network.test.id
  server_id          = pidginhost_server.test.id
  %s
}
`, addr)
}

func TestUnitPrivateNetworkAttachment_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	base := f.ProviderConfig() + privateNetworkConfig("10.40.0.0/24", "10.40.0.1") + acctest.FakeServerConfig("test", "pn-member")
	const name = "pidginhost_private_network_attachment.test"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + attachmentConfig(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					// The gateway takes .1, so the first free address is .2.
					resource.TestCheckResourceAttr(name, "address", "10.40.0.2"),
					resource.TestCheckResourceAttr(name, "interface", "eth1"),
					resource.TestCheckResourceAttr("pidginhost_server.test", "private_networks.#", "0"),
				),
			},
			{
				// The server's computed private_networks reflect the attachment on refresh.
				Config: base + attachmentConfig(""),
				Check:  resource.TestCheckResourceAttr("pidginhost_server.test", "private_networks.0.address", "10.40.0.2"),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				// A fixed address forces a new attachment.
				Config: base + attachmentConfig("10.40.0.50"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("pidginhost_server.test", plancheck.ResourceActionNoop),
				}},
				Check: resource.TestCheckResourceAttr(name, "address", "10.40.0.50"),
			},
			{
				// Removed outside Terraform: detected and re-attached.
				PreConfig: func() { f.RemoveFromPrivateNetwork(f.PrivateNetworkIDs()[0], f.ServerByHostname("pn-member")) },
				Config:    base + attachmentConfig("10.40.0.50"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestUnitPrivateNetworkAttachment_badImportID(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + `
resource "pidginhost_private_network_attachment" "test" {
  private_network_id = "1"
  server_id          = "2"
}
`,
			ResourceName:  "pidginhost_private_network_attachment.test",
			ImportState:   true,
			ImportStateId: "1-2",
			ExpectError:   regexp.MustCompile(`<private_network_id>/<server_id>`),
		}},
	})
}

func TestAccPrivateNetworkAttachment_basic(t *testing.T) {
	cidr := testAccCIDR()
	fixed := cidr[:len(cidr)-4] + "50"
	base := acctest.CatalogConfig() + privateNetworkConfig(cidr, "") + acctest.AccServerConfig("test", acctest.RandomName("pn"))
	const name = "pidginhost_private_network_attachment.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + attachmentConfig(""),
				Check:  resource.TestCheckResourceAttrSet(name, "address"),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				Config: base + attachmentConfig(fixed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("pidginhost_server.test", plancheck.ResourceActionNoop),
				}},
				Check: resource.TestCheckResourceAttr(name, "address", fixed),
			},
		},
	})
}
