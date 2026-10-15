// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package catalog_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
)

func TestUnitCatalogDataSources(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + `
data "pidginhost_images" "all" {}
data "pidginhost_images" "ubuntu" { family_name = "Ubuntu" }
data "pidginhost_server_packages" "all" {}
data "pidginhost_server_packages" "gen1" { generation = "gen1" }
data "pidginhost_storage_products" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// four images across two pages
					resource.TestCheckResourceAttr("data.pidginhost_images.all", "images.#", "4"),
					resource.TestCheckResourceAttr("data.pidginhost_images.all", "images.0.slug", "ubuntu-24-04"),
					resource.TestCheckResourceAttr("data.pidginhost_images.all", "images.0.default_username", "ubuntu"),
					resource.TestCheckNoResourceAttr("data.pidginhost_images.all", "images.3.family_name"),
					resource.TestCheckResourceAttr("data.pidginhost_images.ubuntu", "images.#", "2"),
					resource.TestCheckResourceAttr("data.pidginhost_server_packages.all", "packages.#", "3"),
					resource.TestCheckResourceAttr("data.pidginhost_server_packages.all", "packages.0.cpus", "1"),
					resource.TestCheckResourceAttr("data.pidginhost_server_packages.all", "packages.0.available_generations.#", "2"),
					resource.TestCheckResourceAttr("data.pidginhost_server_packages.gen1", "packages.#", "2"),
					resource.TestCheckResourceAttr("data.pidginhost_storage_products.all", "products.#", "2"),
					resource.TestCheckResourceAttr("data.pidginhost_storage_products.all", "products.0.price", "0.0500"),
				),
			},
		},
	})
}

func TestUnitProviderRejectsBadToken(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
provider "pidginhost" {
  token   = "wrong"
  api_url = "` + f.URL + `"
}
data "pidginhost_images" "all" {}
`,
			ExpectError: regexp.MustCompile(`Invalid token`),
		}},
	})
}

func TestUnitProviderRequiresToken(t *testing.T) {
	t.Setenv("PIDGINHOST_TOKEN", "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `data "pidginhost_images" "all" {}`,
			ExpectError: regexp.MustCompile(`Missing API token`),
		}},
	})
}

func TestUnitProviderTokenFromEnv(t *testing.T) {
	f := acctest.NewFake(t)
	t.Setenv("PIDGINHOST_TOKEN", "fake-test-token")
	t.Setenv("PIDGINHOST_API_URL", f.URL)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: `data "pidginhost_storage_products" "all" {}`,
			Check:  resource.TestCheckResourceAttr("data.pidginhost_storage_products.all", "products.#", "2"),
		}},
	})
}

func TestAccCatalogDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acctest.CatalogConfig() + `
output "image"   { value = local.acc_image }
output "package" { value = local.acc_package }
output "storage" { value = local.acc_storage_product }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.pidginhost_images.all", "images.0.slug"),
				resource.TestCheckResourceAttrSet("data.pidginhost_server_packages.all", "packages.0.slug"),
				resource.TestCheckResourceAttrSet("data.pidginhost_storage_products.all", "products.0.slug"),
			),
		}},
	})
}
