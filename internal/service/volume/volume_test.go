// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package volume_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mihai-amihailesei/pidgin-host/internal/acctest"
)

func volumeConfig(server, product string, size any, alias string) string {
	return fmt.Sprintf(`
resource "pidginhost_volume" "test" {
  server_id = pidginhost_server.%s.id
  product   = %s
  size      = %v
  alias     = %q
}
`, server, product, size, alias)
}

func action(name string, a plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, a)}}
}

func TestUnitVolume_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	servers := f.ProviderConfig() + acctest.FakeServerConfig("a", "vol-a") + acctest.FakeServerConfig("b", "vol-b")
	const name = "pidginhost_volume.test"
	var firstID string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: servers + volumeConfig("a", `"ssd"`, 20, "data"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size", "20"),
					resource.TestCheckResourceAttr(name, "attached", "true"),
					resource.TestCheckResourceAttr(name, "server_hostname", "vol-a"),
					resource.TestCheckResourceAttrPair(name, "server_id", "pidginhost_server.a", "id"),
					func(s *terraform.State) (err error) { firstID, err = acctest.AttrFromState(s, name, "id"); return },
				),
			},
			{
				// Growing and renaming: in place.
				Config:           servers + volumeConfig("a", `"ssd"`, 30, "data-2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "size", "30"),
					resource.TestCheckResourceAttr(name, "alias", "data-2"),
					resource.TestCheckResourceAttrWith(name, "id", func(v string) error {
						if v != firstID {
							return fmt.Errorf("volume was replaced: %s -> %s", firstID, v)
						}
						return nil
					}),
				),
			},
			{
				// Moving to another server: in place, same volume.
				Config:           servers + volumeConfig("b", `"ssd"`, 30, "data-2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "server_hostname", "vol-b"),
					resource.TestCheckResourceAttrPair(name, "server_id", "pidginhost_server.b", "id"),
					resource.TestCheckResourceAttrWith(name, "id", func(v string) error {
						if v != firstID {
							return fmt.Errorf("volume was replaced: %s -> %s", firstID, v)
						}
						return nil
					}),
				),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "product"},
			},
			{
				// Detached outside Terraform: re-attached in place.
				PreConfig: func() {
					for _, id := range f.VolumeIDs() {
						f.DetachVolume(id)
					}
				},
				Config:           servers + volumeConfig("b", `"ssd"`, 30, "data-2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttr(name, "attached", "true"),
			},
			{
				// Shrinking is impossible: replacement.
				Config:           servers + volumeConfig("b", `"ssd"`, 20, "data-2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionDestroyBeforeCreate),
			},
			{
				// So is changing the product.
				Config:           servers + volumeConfig("b", `"hdd"`, 50, "data-2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionDestroyBeforeCreate),
			},
		},
	})
}

func TestAccVolume_basic(t *testing.T) {
	alias := acctest.RandomName("vol")
	var initialSize int
	base := acctest.CatalogConfig() + acctest.AccServerConfig("a", acctest.RandomName("vol"))
	const name = "pidginhost_volume.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: base + volumeConfig("a", "local.acc_storage_product", "local.acc_volume_size", alias),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "attached", "true"),
					resource.TestCheckResourceAttrWith(name, "size", func(v string) (err error) {
						_, err = fmt.Sscan(v, &initialSize)
						return err
					}),
				),
			},
			{
				Config:           base + volumeConfig("a", "local.acc_storage_product", "local.acc_volume_size_grown", alias+"-2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "size", func(v string) error {
						if v != fmt.Sprint(initialSize+10) {
							return fmt.Errorf("size = %s, want %d", v, initialSize+10)
						}
						return nil
					}),
					resource.TestCheckResourceAttr(name, "alias", alias+"-2"),
				),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "product"},
			},
		},
	})
}

// After import, a product given in another spelling (the API reports the ID
// "21", the configuration says "ssd") must be adopted, never trigger a
// replacement that would destroy the volume's data.
func TestUnitVolume_importAdoptsProduct(t *testing.T) {
	f := acctest.NewFake(t)
	srv := f.CreateServer("vol-imp", "debian-12", "cloud-xs")
	vid := f.CreateVolumeOn(srv, "21", 20, "data")
	cfg := f.ProviderConfig() + fmt.Sprintf(`
resource "pidginhost_volume" "test" {
  server_id = "%d"
  product   = "ssd"
  size      = 20
  alias     = "data"
}
`, srv)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             cfg,
				ResourceName:       "pidginhost_volume.test",
				ImportState:        true,
				ImportStateId:      fmt.Sprint(vid),
				ImportStatePersist: true,
			},
			{
				Config:           cfg,
				ConfigPlanChecks: action("pidginhost_volume.test", plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttr("pidginhost_volume.test", "id", fmt.Sprint(vid)),
			},
			{
				// Once adopted, changing the product replaces as usual.
				Config:           strings.Replace(cfg, `"ssd"`, `"hdd"`, 1),
				ConfigPlanChecks: action("pidginhost_volume.test", plancheck.ResourceActionDestroyBeforeCreate),
			},
		},
	})
}
