// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package objectstorage_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
)

func credentialsConfig(bucket, trigger string) string {
	t := ""
	if trigger != "" {
		t = fmt.Sprintf("rotation_trigger = %q", trigger)
	}
	return bucketConfig(bucket, 1, false) + fmt.Sprintf(`
resource "pidginhost_bucket_credentials" "test" {
  bucket_id = pidginhost_bucket.test.id
  %s
}
`, t)
}

// keyTracker returns a check that records the access key and verifies whether it
// changed since the previous check.
func keyTracker(name string) func(wantChange bool) resource.TestCheckFunc {
	var last string
	return func(wantChange bool) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			key, err := acctest.AttrFromState(s, name, "access_key")
			if err != nil {
				return err
			}
			changed := key != last
			prev := last
			last = key
			if prev != "" && changed != wantChange {
				return fmt.Errorf("access key changed = %t, want %t (%q -> %q)", changed, wantChange, prev, key)
			}
			return nil
		}
	}
}

func TestUnitBucketCredentials_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	const name = "pidginhost_bucket_credentials.test"
	key := keyTracker(name)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + credentialsConfig("creds", "v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(name, "secret_key"),
					resource.TestCheckResourceAttr(name, "bucket", "acct42-creds"),
					resource.TestCheckResourceAttr(name, "endpoint", "https://s3.fake.invalid"),
					key(false),
				),
			},
			{
				// Same trigger: no rotation, no change.
				Config: f.ProviderConfig() + credentialsConfig("creds", "v1"),
				Check:  key(false),
			},
			{
				Config:           f.ProviderConfig() + credentialsConfig("creds", "v2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check:            key(true),
			},
			{
				// Rotated outside Terraform: refresh picks the new keys up.
				PreConfig: func() { f.RotateBucketKeys(f.BucketIDs()[0]) },
				Config:    f.ProviderConfig() + credentialsConfig("creds", "v2"),
				Check:     key(true),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "rotation_trigger"},
			},
			{
				// Removing the trigger is a change too.
				Config:           f.ProviderConfig() + credentialsConfig("creds", ""),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check:            key(true),
			},
		},
	})
	if n := f.CountRequests("POST", "/credentials/rotate/"); n != 2 {
		t.Fatalf("rotate called %d times, want 2", n)
	}
}

// Importing credentials and configuring a rotation_trigger must not rotate
// the live keys.
func TestUnitBucketCredentials_importDoesNotRotate(t *testing.T) {
	f := acctest.NewFake(t)
	const name = "pidginhost_bucket_credentials.test"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{Config: f.ProviderConfig() + bucketConfig("imp", 1, false)},
			{
				Config:       f.ProviderConfig() + credentialsConfig("imp", "v1"),
				ResourceName: name,
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return acctest.AttrFromState(s, "pidginhost_bucket.test", "id")
				},
				ImportStatePersist: true,
			},
			{
				Config:           f.ProviderConfig() + credentialsConfig("imp", "v1"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
			},
			{
				// After adoption, changing the trigger rotates.
				Config: f.ProviderConfig() + credentialsConfig("imp", "v2"),
			},
		},
	})
	if n := f.CountRequests("POST", "/credentials/rotate/"); n != 1 {
		t.Fatalf("rotate called %d times, want 1 (only for the v1 -> v2 change)", n)
	}
}

func TestAccBucketCredentials_basic(t *testing.T) {
	bucket := acctest.RandomName("bkt")
	const name = "pidginhost_bucket_credentials.test"
	key := keyTracker(name)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: credentialsConfig(bucket, "v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(name, "secret_key"),
					key(false),
				),
			},
			{
				Config:           credentialsConfig(bucket, "v2"),
				ConfigPlanChecks: action(name, plancheck.ResourceActionUpdate),
				Check:            key(true),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "rotation_trigger"},
			},
		},
	})
}
