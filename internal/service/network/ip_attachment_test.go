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

func ipAttachmentConfig(family, server string, reboot bool) string {
	return fmt.Sprintf(`
resource "pidginhost_%[1]s" "test" {}

resource "pidginhost_%[1]s_attachment" "test" {
  server_id  = pidginhost_server.%[2]s.id
  address_id = pidginhost_%[1]s.test.id
  reboot     = %[3]t
}
`, family, server, reboot)
}

func testUnitIPAttachment(t *testing.T, family string, v6 bool) {
	f := acctest.NewFake(t)
	servers := f.ProviderConfig() + acctest.FakeServerConfig("a", "ip-a") + acctest.FakeServerConfig("b", "ip-b")
	att := "pidginhost_" + family + "_attachment.test"
	ip := "pidginhost_" + family + ".test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: servers + ipAttachmentConfig(family, "a", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(att, "reboot_required", "true"),
				),
			},
			{
				// The address now reports the server it is attached to.
				Config: servers + ipAttachmentConfig(family, "a", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(ip, "server_id", "pidginhost_server.a", "id"),
					resource.TestCheckResourceAttr(ip, "attached", "true"),
				),
			},
			{
				// Changing reboot does not touch the attachment.
				Config: servers + ipAttachmentConfig(family, "a", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(att, plancheck.ResourceActionUpdate),
				}},
			},
			{
				ResourceName:            att,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "reboot", "reboot_required"},
			},
			{
				// Moving to another server re-attaches, keeping the address.
				Config: servers + ipAttachmentConfig(family, "b", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(att, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction(ip, plancheck.ResourceActionNoop),
				}},
			},
			{
				// Detached outside Terraform: re-attached.
				PreConfig: func() { f.DetachAllIPs(v6, f.ServerByHostname("ip-b")) },
				Config:    servers + ipAttachmentConfig(family, "b", true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(att, plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestUnitIPv4Attachment_lifecycle(t *testing.T) { testUnitIPAttachment(t, "ipv4", false) }

func TestUnitIPv6Attachment_lifecycle(t *testing.T) { testUnitIPAttachment(t, "ipv6", true) }

func testAccIPAttachment(t *testing.T, family string) {
	base := acctest.CatalogConfig() + acctest.AccServerConfig("a", acctest.RandomName("ipatt"))
	att := "pidginhost_" + family + "_attachment.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + ipAttachmentConfig(family, "a", false),
				Check:  resource.TestCheckResourceAttrPair(att, "server_id", "pidginhost_server.a", "id"),
			},
			{
				// Refresh shows the address attached.
				Config: base + ipAttachmentConfig(family, "a", false),
				Check:  resource.TestCheckResourceAttr("pidginhost_"+family+".test", "attached", "true"),
			},
			{
				ResourceName:            att,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "reboot", "reboot_required"},
			},
		},
	})
}

func TestAccIPv4Attachment_basic(t *testing.T) { testAccIPAttachment(t, "ipv4") }

func TestAccIPv6Attachment_basic(t *testing.T) { testAccIPAttachment(t, "ipv6") }
