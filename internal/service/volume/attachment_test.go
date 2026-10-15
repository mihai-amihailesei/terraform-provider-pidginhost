// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package volume_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
)

func TestUnitVolumeAttachment_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	vid := f.CreateDetachedVolume("panel-made", 40)
	servers := f.ProviderConfig() + acctest.FakeServerConfig("a", "va-a") + acctest.FakeServerConfig("b", "va-b")
	const name = "pidginhost_volume_attachment.test"
	cfg := func(server string) string {
		return servers + fmt.Sprintf(`
resource "pidginhost_volume_attachment" "test" {
  volume_id = "%d"
  server_id = pidginhost_server.%s.id
}
`, vid, server)
	}
	attachedTo := func(host string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if got, want := f.VolumeServer(vid), f.ServerByHostname(host); got != want {
				return fmt.Errorf("volume attached to %d, want %d", got, want)
			}
			return nil
		}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("a"), Check: attachedTo("va-a")},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				Config:           cfg("b"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionDestroyBeforeCreate),
				Check:            attachedTo("va-b"),
			},
			{
				PreConfig:        func() { f.DetachVolume(vid) },
				Config:           cfg("b"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionCreate),
				Check:            attachedTo("va-b"),
			},
		},
	})
	// Destroying the attachment detached the volume but kept it.
	if f.VolumeServer(vid) != 0 {
		t.Fatal("volume still attached after destroy")
	}
}

// pidginhost_volume_attachment must refuse to steal a volume attached elsewhere.

func TestUnitVolumeAttachment_refusesToMove(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + acctest.FakeServerConfig("a", "va-a") + acctest.FakeServerConfig("b", "va-b") +
				volumeConfig("a", `"ssd"`, 20, "data") + `
resource "pidginhost_volume_attachment" "steal" {
  volume_id = pidginhost_volume.test.id
  server_id = pidginhost_server.b.id
}
`,
			ExpectError: regexpMust(`attached to "va-a"`),
		}},
	})
}

// TestAccVolumeAttachment_basic checks adoption and import of an attachment
// against a real volume.
func TestAccVolumeAttachment_basic(t *testing.T) {
	cfg := func() string {
		c := acctest.CatalogConfig() + acctest.AccServerConfig("b", acctest.RandomName("va"))
		c += fmt.Sprintf(`
resource "pidginhost_volume" "test" {
  server_id = pidginhost_server.b.id
  product   = local.acc_storage_product
  size      = local.acc_volume_size
  alias     = %q
}
`, acctest.RandomName("va"))
		return c
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Adopting an attachment that already exists is a no-op.
				Config: cfg() + `
resource "pidginhost_volume_attachment" "test" {
  volume_id = pidginhost_volume.test.id
  server_id = pidginhost_server.b.id
}
`,
				Check: resource.TestCheckResourceAttrPair("pidginhost_volume_attachment.test", "server_id", "pidginhost_server.b", "id"),
			},
			{
				ResourceName:            "pidginhost_volume_attachment.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
