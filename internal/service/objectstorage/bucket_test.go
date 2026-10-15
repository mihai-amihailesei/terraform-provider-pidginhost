// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package objectstorage_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/mihai-amihailesei/pidgin-host/internal/acctest"
)

func bucketConfig(name string, quota int, public bool) string {
	return fmt.Sprintf(`
resource "pidginhost_bucket" "test" {
  name        = %q
  quota_gb    = %d
  public_read = %t
}
`, name, quota, public)
}

func action(name string, a plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, a)}}
}

func TestUnitBucket_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	const name = "pidginhost_bucket.test"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + bucketConfig("assets", 5, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					resource.TestCheckResourceAttr(name, "full_name", "acct42-assets"),
					resource.TestCheckResourceAttr(name, "endpoint", "https://s3.fake.invalid"),
					resource.TestCheckResourceAttr(name, "region", "ro-buh-1"),
					// Larger than int32: decoded through the SDK workaround.
					resource.TestCheckResourceAttr(name, "used_bytes", "5368709120"),
				),
			},
			{
				Config:           f.ProviderConfig() + bucketConfig("assets", 10, true),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "quota_gb", "10"),
					resource.TestCheckResourceAttr(name, "public_read", "true"),
				),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				PreConfig:        func() { f.SetBucketPublic(f.BucketIDs()[0], false) },
				Config:           f.ProviderConfig() + bucketConfig("assets", 10, true),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
			},
			{
				Config:           f.ProviderConfig() + bucketConfig("assets-2", 10, true),
				ConfigPlanChecks: action(name, plancheck.ResourceActionDestroyBeforeCreate),
			},
		},
	})
	if ids := f.BucketIDs(); len(ids) != 0 {
		t.Fatalf("buckets left behind: %v", ids)
	}
}

func TestUnitBucket_apiValidation(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config:      f.ProviderConfig() + bucketConfig("Not_Valid", 5, false),
			ExpectError: regexp.MustCompile(`name: Use lowercase letters`),
		}},
	})
}

func TestUnitBucketDataSource(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + bucketConfig("lookup", 5, false) + `
data "pidginhost_bucket" "by_id" { id = pidginhost_bucket.test.id }
data "pidginhost_bucket" "by_name" { name = pidginhost_bucket.test.name }
data "pidginhost_bucket" "by_full_name" { name = pidginhost_bucket.test.full_name }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.pidginhost_bucket.by_id", "full_name", "pidginhost_bucket.test", "full_name"),
				resource.TestCheckResourceAttrPair("data.pidginhost_bucket.by_name", "id", "pidginhost_bucket.test", "id"),
				resource.TestCheckResourceAttrPair("data.pidginhost_bucket.by_full_name", "id", "pidginhost_bucket.test", "id"),
				resource.TestCheckResourceAttr("data.pidginhost_bucket.by_name", "used_bytes", "5368709120"),
			),
		}},
	})
}

func TestAccBucket_basic(t *testing.T) {
	bucket := acctest.RandomName("bkt")
	const name = "pidginhost_bucket.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: bucketConfig(bucket, 1, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					resource.TestCheckResourceAttrSet(name, "endpoint"),
					resource.TestCheckResourceAttrSet(name, "full_name"),
				),
			},
			{
				Config:           bucketConfig(bucket, 2, true),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "quota_gb", "2"),
					resource.TestCheckResourceAttr(name, "public_read", "true"),
				),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "used_bytes", "object_count"},
			},
		},
	})
}

func TestAccBucketDataSource_basic(t *testing.T) {
	bucket := acctest.RandomName("bkt")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: bucketConfig(bucket, 1, false) + `
data "pidginhost_bucket" "by_name" { name = pidginhost_bucket.test.name }
`,
			Check: resource.TestCheckResourceAttrPair("data.pidginhost_bucket.by_name", "id", "pidginhost_bucket.test", "id"),
		}},
	})
}
