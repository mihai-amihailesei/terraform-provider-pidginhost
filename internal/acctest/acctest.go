// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package acctest holds helpers shared by the provider's tests.
//
// Two kinds of test use it:
//
//   - Unit tests (TestUnit*), which run terraform against the in-memory fake
//     API in internal/fakeapi. They need no credentials and run in CI.
//   - Acceptance tests (TestAcc*), which create real, billable resources.
//     They run only with TF_ACC=1 and PIDGINHOST_TOKEN set. Every resource
//     they create is named with NamePrefix so that the sweepers in
//     internal/sweep can find and delete leftovers.
package acctest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mihai-amihailesei/pidgin-host/internal/apiclient"
	"github.com/mihai-amihailesei/pidgin-host/internal/fakeapi"
	"github.com/mihai-amihailesei/pidgin-host/internal/provider"
)

// NamePrefix starts the name of every resource an acceptance test creates.
const NamePrefix = "tfacc-"

// Environment variables that tune the acceptance tests.
const (
	// EnvImage overrides the OS image (ID or slug) used for test servers.
	EnvImage = "PIDGINHOST_ACC_IMAGE"
	// EnvPackage overrides the server package (ID or slug). By default the
	// package with the least memory is used.
	EnvPackage = "PIDGINHOST_ACC_PACKAGE"
	// EnvPackageUpgrade is the package used to test in-place package changes.
	// By default the second-smallest package is used.
	EnvPackageUpgrade = "PIDGINHOST_ACC_PACKAGE_UPGRADE"
	// EnvStorageProduct overrides the storage product (ID or slug) used for
	// volumes. By default the first product is used.
	EnvStorageProduct = "PIDGINHOST_ACC_STORAGE_PRODUCT"
	// EnvFake, when "1", runs the acceptance tests against the in-memory
	// fake API instead of the real one. It costs nothing and checks the
	// tests themselves; it proves nothing about the real API.
	EnvFake = "PIDGINHOST_ACC_FAKE"
)

// ProviderFactories serves the real provider, configured from the
// environment (PIDGINHOST_TOKEN, PIDGINHOST_API_URL).
var ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"pidginhost": providerserver.NewProtocol6WithError(provider.New("test", accOptions()...)()),
}

func accOptions() []provider.Option {
	if os.Getenv(EnvFake) == "1" {
		return []provider.Option{provider.WithPollInterval(10 * time.Millisecond)}
	}
	return nil
}

// FakeProviderFactories serves the provider with fast polling, for use with
// the fake API (whose URL and token are set in the provider block returned by
// fakeapi.Server.ProviderConfig).
var FakeProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"pidginhost": providerserver.NewProtocol6WithError(provider.New("test", provider.WithPollInterval(10*time.Millisecond))()),
}

// PreCheck fails the test early when acceptance-test credentials are missing.
// With PIDGINHOST_ACC_FAKE=1 it starts a fake API for the test instead.
func PreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv(EnvFake) == "1" {
		f := fakeapi.New(t)
		t.Setenv(provider.EnvToken, fakeapi.Token)
		t.Setenv(provider.EnvAPIURL, f.URL)
		return
	}
	if os.Getenv(provider.EnvToken) == "" {
		t.Fatalf("%s must be set for acceptance tests", provider.EnvToken)
	}
}

// RandomName returns NamePrefix followed by the test's kind and a random
// suffix, e.g. "tfacc-server-k3j9x2".
func RandomName(kind string) string {
	return NamePrefix + kind + "-" + strings.ToLower(acctest.RandString(6))
}

// Client builds an API client from the environment, for test checks and
// sweepers.
func Client() (*apiclient.Client, error) {
	return apiclient.New(apiclient.Config{
		Token:     os.Getenv(provider.EnvToken),
		BaseURL:   os.Getenv(provider.EnvAPIURL),
		UserAgent: "terraform-provider-pidginhost/acctest",
	})
}

// NewFake starts a fake API for a unit test.
func NewFake(t *testing.T) *fakeapi.Server {
	t.Helper()
	return fakeapi.New(t)
}

// Getenv returns the value of key, or def when it is unset.
func Getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// AttrFromState returns the value of attr of resource name in s.
func AttrFromState(s *terraform.State, name, attr string) (string, error) {
	rs, ok := s.RootModule().Resources[name]
	if !ok {
		return "", fmt.Errorf("resource %s not found in state", name)
	}
	v, ok := rs.Primary.Attributes[attr]
	if !ok {
		return "", fmt.Errorf("attribute %s not found on %s", attr, name)
	}
	return v, nil
}

// Context returns a context for API calls made by test checks.
func Context() context.Context {
	return context.Background()
}

// CatalogConfig declares locals with an image, the two smallest packages and
// a storage product, chosen from the catalog data sources unless overridden
// through the EnvImage, EnvPackage, EnvPackageUpgrade and EnvStorageProduct
// environment variables. Acceptance tests build on it so that they always
// use the cheapest server package available.
func CatalogConfig() string {
	return fmt.Sprintf(`
data "pidginhost_images" "all" {}
data "pidginhost_server_packages" "all" {}
data "pidginhost_storage_products" "all" {}

locals {
  acc_image_override           = %q
  acc_package_override         = %q
  acc_package_upgrade_override = %q
  acc_storage_override         = %q

  acc_debian_like = [for i in data.pidginhost_images.all.images : i.slug if can(regex("(?i)(debian|ubuntu)", i.name))]
  acc_image       = local.acc_image_override != "" ? local.acc_image_override : local.acc_debian_like[0]

  acc_packages_by_size = [for p in data.pidginhost_server_packages.all.packages : format("%%012d|%%012d|%%s", p.memory, p.cpus, p.slug)]
  acc_sorted_packages  = [for k in sort(local.acc_packages_by_size) : split("|", k)[2]]
  acc_package          = local.acc_package_override != "" ? local.acc_package_override : local.acc_sorted_packages[0]
  acc_package_upgrade  = local.acc_package_upgrade_override != "" ? local.acc_package_upgrade_override : local.acc_sorted_packages[1]

  acc_storage_product = local.acc_storage_override != "" ? local.acc_storage_override : data.pidginhost_storage_products.all.products[0].slug
  acc_storage_min     = one([for p in data.pidginhost_storage_products.all.products : p.min_size if p.slug == local.acc_storage_product || p.id == local.acc_storage_product])
  # Smallest volume the product allows, and a slightly larger one for resize tests.
  acc_volume_size       = max(local.acc_storage_min, 1)
  acc_volume_size_grown = local.acc_volume_size + 10
}
`, os.Getenv(EnvImage), os.Getenv(EnvPackage), os.Getenv(EnvPackageUpgrade), os.Getenv(EnvStorageProduct))
}

// FakeServerConfig declares a pidginhost_server resource named name for use
// against the fake API.
func FakeServerConfig(name, hostname string) string {
	return fmt.Sprintf(`
resource "pidginhost_server" %q {
  hostname = %q
  image    = "debian-12"
  package  = "cloud-xs"
  new_ipv4 = true
}
`, name, hostname)
}

// AccServerConfig declares the cheapest possible pidginhost_server resource
// named name. It needs CatalogConfig in the same configuration.
func AccServerConfig(name, hostname string) string {
	return fmt.Sprintf(`
resource "pidginhost_server" %q {
  hostname = %q
  image    = local.acc_image
  package  = local.acc_package
  new_ipv4 = true
}
`, name, hostname)
}
