// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package server_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mihai-amihailesei/pidgin-host/internal/acctest"
	"github.com/mihai-amihailesei/pidgin-host/internal/fakeapi"
)

// serverOpts drives serverConfig. Empty fields are omitted.
type serverOpts struct {
	Hostname          string
	Image             string // HCL expression
	Package           string // HCL expression
	Project           string
	Firewall          bool
	PolicyIn          string
	PowerState        string
	DestroyProtection bool
	UserData          string
	Extra             string
}

func serverConfig(o serverOpts) string {
	var b strings.Builder
	if o.Firewall {
		b.WriteString(`
resource "pidginhost_firewall_rules_set" "test" {
  name = "` + o.Hostname + `"
}
`)
	}
	fmt.Fprintf(&b, `
resource "pidginhost_server" "test" {
  hostname = %q
  image    = %s
  package  = %s
  new_ipv4 = true
`, o.Hostname, o.Image, o.Package)
	if o.Project != "" {
		fmt.Fprintf(&b, "  project = %q\n", o.Project)
	}
	if o.Firewall {
		b.WriteString("  firewall_rules_set_id = pidginhost_firewall_rules_set.test.id\n")
	}
	if o.PolicyIn != "" {
		fmt.Fprintf(&b, "  firewall_policy_in = %q\n", o.PolicyIn)
	}
	if o.PowerState != "" {
		fmt.Fprintf(&b, "  power_state = %q\n", o.PowerState)
	}
	if o.DestroyProtection {
		b.WriteString("  destroy_protection = true\n")
	}
	if o.UserData != "" {
		fmt.Fprintf(&b, "  user_data = %q\n", o.UserData)
	}
	b.WriteString(o.Extra)
	b.WriteString("}\n")
	return b.String()
}

// expectUpdate checks that pidginhost_server.test is updated in place.
func expectUpdate() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("pidginhost_server.test", plancheck.ResourceActionUpdate),
	}}
}

func checkFake(f *fakeapi.Server, fn func(f *fakeapi.Server) error) resource.TestCheckFunc {
	return func(*terraform.State) error { return fn(f) }
}

// importIgnore lists create-only attributes the API never returns.
var importIgnore = []string{"image", "user_data", "new_ipv4", "password", "ssh_key_id", "timeouts"}

func TestUnitServer_lifecycle(t *testing.T) {
	f := acctest.NewFake(t)
	base := serverOpts{
		Hostname: "web-1", Image: `"ubuntu-24-04"`, Package: `"cloud-xs"`, Project: "blue",
		Firewall: true, UserData: "#!/bin/sh\necho hello\n",
	}
	step := func(mod func(o *serverOpts)) string {
		o := base
		if mod != nil {
			mod(&o)
		}
		return f.ProviderConfig() + serverConfig(o)
	}
	const name = "pidginhost_server.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: step(nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "status", "active"),
					resource.TestCheckResourceAttr(name, "hostname", "web-1"),
					resource.TestCheckResourceAttr(name, "image_name", "Ubuntu 24.04"),
					resource.TestCheckResourceAttr(name, "package", "cloud-xs"),
					resource.TestCheckResourceAttr(name, "cpus", "1"),
					resource.TestCheckResourceAttr(name, "username", "ubuntu"),
					resource.TestCheckResourceAttr(name, "power_state", "running"),
					resource.TestCheckResourceAttr(name, "destroy_protection", "false"),
					resource.TestCheckResourceAttr(name, "firewall_policy_in", "ACCEPT"),
					resource.TestCheckResourceAttrPair(name, "firewall_rules_set_id", "pidginhost_firewall_rules_set.test", "id"),
					resource.TestMatchResourceAttr(name, "ipv4_address", regexp.MustCompile(`^192\.0\.2\.`)),
					resource.TestCheckResourceAttr(name, "generation", "gen2"),
					checkFake(f, func(f *fakeapi.Server) error {
						if got := f.ServerUserData(f.ServerByHostname("web-1")); got != base.UserData {
							return fmt.Errorf("user_data sent to the API = %q", got)
						}
						return nil
					}),
				),
			},
			{
				// Package change: in place, through modify-package.
				Config:           step(func(o *serverOpts) { o.Package = `"cloud-s"` }),
				ConfigPlanChecks: expectUpdate(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "package", "cloud-s"),
					resource.TestCheckResourceAttr(name, "cpus", "2"),
					checkFake(f, func(f *fakeapi.Server) error {
						if n := f.CountRequests("POST", "/modify-package/"); n != 1 {
							return fmt.Errorf("modify-package called %d times", n)
						}
						return nil
					}),
				),
			},
			{
				// Project, firewall policy, power state and destroy
				// protection: all in place.
				Config: step(func(o *serverOpts) {
					o.Package, o.Project, o.PolicyIn, o.PowerState, o.DestroyProtection = `"cloud-s"`, "green", "DROP", "stopped", true
				}),
				ConfigPlanChecks: expectUpdate(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "project", "green"),
					resource.TestCheckResourceAttr(name, "firewall_policy_in", "DROP"),
					resource.TestCheckResourceAttr(name, "power_state", "stopped"),
					resource.TestCheckResourceAttr(name, "destroy_protection", "true"),
				),
			},
			{
				// Destroying a protected server fails with a clear message.
				Config: step(func(o *serverOpts) {
					o.Package, o.Project, o.PolicyIn, o.PowerState, o.DestroyProtection = `"cloud-s"`, "green", "DROP", "stopped", true
				}),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`destroy_protection enabled`),
			},
			{
				// Detaching the firewall sends an explicit null and
				// starting the server again works.
				Config: step(func(o *serverOpts) {
					o.Package, o.Project, o.PolicyIn, o.PowerState, o.Firewall = `"cloud-s"`, "green", "DROP", "running", false
				}),
				ConfigPlanChecks: expectUpdate(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(name, "firewall_rules_set_id"),
					resource.TestCheckResourceAttr(name, "power_state", "running"),
					resource.TestCheckResourceAttr(name, "destroy_protection", "false"),
					checkFake(f, func(f *fakeapi.Server) error {
						for _, r := range f.Requests() {
							if r.Method == "POST" && strings.HasSuffix(r.Path, "/public-interface/") {
								if v, present := r.Body["fw_rules_set"]; present && v == nil {
									return nil
								}
							}
						}
						return fmt.Errorf("no public-interface request sent fw_rules_set: null")
					}),
				),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: importIgnore,
			},
			{
				// Changing user_data (create-only) replaces the server.
				Config: step(func(o *serverOpts) {
					o.Package, o.Project, o.PolicyIn, o.UserData = `"cloud-s"`, "green", "DROP", "#!/bin/sh\necho changed\n"
				}),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

func TestUnitServer_drift(t *testing.T) {
	f := acctest.NewFake(t)
	cfg := f.ProviderConfig() + serverConfig(serverOpts{Hostname: "drift", Image: `"debian-12"`, Package: `"cloud-xs"`, Project: "p", Firewall: true})
	const name = "pidginhost_server.test"
	id := func() int32 { return f.ServerByHostname("drift") }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig:        func() { f.SetServerProject(id(), "elsewhere") },
				Config:           cfg,
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttr(name, "project", "p"),
			},
			{
				PreConfig:        func() { f.SetServerPackage(id(), "cloud-m") },
				Config:           cfg,
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttr(name, "package", "cloud-xs"),
			},
			{
				PreConfig:        func() { f.DetachServerFirewall(id()) },
				Config:           cfg,
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttrPair(name, "firewall_rules_set_id", "pidginhost_firewall_rules_set.test", "id"),
			},
			{
				// Deleted outside Terraform: removed from state and created again.
				PreConfig: func() { f.DeleteServer(id()) },
				Config:    cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(name, plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

// A package given by ID must not cause a permanent diff when the API reports
// the slug.
func TestUnitServer_packageByID(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + serverConfig(serverOpts{Hostname: "byid", Image: `"3"`, Package: `"11"`}),
			Check:  resource.TestCheckResourceAttr("pidginhost_server.test", "package", "11"),
		}},
	})
}

func TestUnitServer_provisioningFailure(t *testing.T) {
	f := acctest.NewFake(t)
	f.FailProvisioning = true
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config:      f.ProviderConfig() + serverConfig(serverOpts{Hostname: "broken", Image: `"debian-12"`, Package: `"cloud-xs"`}),
			ExpectError: regexp.MustCompile(`reached status "failed"`),
		}},
	})
	// The failed server was tracked in state and destroyed by the test
	// framework, not left behind.
	if ids := f.ServerIDs(); len(ids) != 0 {
		t.Fatalf("servers left behind: %v", ids)
	}
}

// After import, create-only attributes can be added to the configuration
// without replacing the server.
//
// The API reports the hostname as an FQDN and the package as a slug while
// the configuration uses the short name and the package ID: neither may
// cause a replacement or a modify-package call.
func TestUnitServer_importThenConfigureCreateOnly(t *testing.T) {
	f := acctest.NewFake(t)
	id := f.CreateServer("imported.example.com", "debian-12", "cloud-xs")
	cfg := f.ProviderConfig() + `
resource "pidginhost_server" "test" {
  hostname  = "imported"
  image     = "debian-12"
  package   = "11"
  generation = "GEN2"
  new_ipv4  = true
  user_data = "#!/bin/sh\ntrue\n"
  password  = "s3cret-Passw0rd"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             cfg,
				ResourceName:       "pidginhost_server.test",
				ImportState:        true,
				ImportStateId:      fmt.Sprint(id),
				ImportStatePersist: true,
			},
			{
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("pidginhost_server.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pidginhost_server.test", "id", fmt.Sprint(id)),
					checkFake(f, func(f *fakeapi.Server) error {
						if n := f.CountRequests("POST", "/modify-package/"); n != 0 {
							return fmt.Errorf("modify-package called %d times for an unchanged package", n)
						}
						return nil
					}),
				),
			},
			{
				// After that first update, changing the hostname for real replaces.
				Config: strings.Replace(cfg, `hostname  = "imported"`, `hostname  = "renamed"`, 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("pidginhost_server.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

func TestUnitServerDataSource(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.ProviderConfig() + serverConfig(serverOpts{Hostname: "lookup", Image: `"debian-12"`, Package: `"cloud-s"`}) + `
data "pidginhost_server" "by_id" { id = pidginhost_server.test.id }
data "pidginhost_server" "by_name" { hostname = pidginhost_server.test.hostname }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.pidginhost_server.by_id", "hostname", "pidginhost_server.test", "hostname"),
				resource.TestCheckResourceAttrPair("data.pidginhost_server.by_name", "id", "pidginhost_server.test", "id"),
				resource.TestCheckResourceAttrPair("data.pidginhost_server.by_name", "ipv4_address", "pidginhost_server.test", "ipv4_address"),
				resource.TestCheckResourceAttr("data.pidginhost_server.by_id", "package", "cloud-s"),
				resource.TestCheckResourceAttr("data.pidginhost_server.by_id", "power_state", "running"),
			),
		}},
	})
}

func TestAccServer_basic(t *testing.T) {
	name := acctest.RandomName("srv")
	base := serverOpts{
		Hostname: name, Image: "local.acc_image", Package: "local.acc_package", Project: acctest.NamePrefix + "project",
		Firewall: true, UserData: "#!/bin/sh\necho tfacc > /root/tfacc\n",
	}
	step := func(mod func(o *serverOpts)) string {
		o := base
		if mod != nil {
			mod(&o)
		}
		return acctest.CatalogConfig() + serverConfig(o)
	}
	const res = "pidginhost_server.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: step(nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "status", "active"),
					resource.TestCheckResourceAttr(res, "hostname", name),
					resource.TestCheckResourceAttrSet(res, "ipv4_address"),
					resource.TestCheckResourceAttr(res, "power_state", "running"),
					resource.TestCheckResourceAttrPair(res, "firewall_rules_set_id", "pidginhost_firewall_rules_set.test", "id"),
				),
			},
			{
				Config:           step(func(o *serverOpts) { o.Project, o.PolicyIn = acctest.NamePrefix+"project-2", "DROP" }),
				ConfigPlanChecks: expectUpdate(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "project", acctest.NamePrefix+"project-2"),
					resource.TestCheckResourceAttr(res, "firewall_policy_in", "DROP"),
				),
			},
			{
				Config:           step(func(o *serverOpts) { o.Project, o.PowerState = acctest.NamePrefix+"project-2", "stopped" }),
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttr(res, "power_state", "stopped"),
			},
			{
				Config:           step(func(o *serverOpts) { o.Project, o.PowerState = acctest.NamePrefix+"project-2", "running" }),
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttr(res, "power_state", "running"),
			},
			{
				Config:           step(func(o *serverOpts) { o.Project, o.DestroyProtection = acctest.NamePrefix+"project-2", true }),
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttr(res, "destroy_protection", "true"),
			},
			{
				Config:           step(func(o *serverOpts) { o.Project, o.Firewall = acctest.NamePrefix+"project-2", false }),
				ConfigPlanChecks: expectUpdate(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "destroy_protection", "false"),
					resource.TestCheckNoResourceAttr(res, "firewall_rules_set_id"),
				),
			},
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: importIgnore,
			},
		},
	})
}

// TestAccServer_packageChange resizes a server in place. It is separate from
// TestAccServer_basic because it bills for the larger package.
func TestAccServer_packageChange(t *testing.T) {
	name := acctest.RandomName("srv")
	cfg := func(pkg string) string {
		return acctest.CatalogConfig() + serverConfig(serverOpts{Hostname: name, Image: "local.acc_image", Package: pkg})
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("local.acc_package")},
			{
				Config:           cfg("local.acc_package_upgrade"),
				ConfigPlanChecks: expectUpdate(),
				Check:            resource.TestCheckResourceAttr("pidginhost_server.test", "status", "active"),
			},
		},
	})
}

func TestAccServerDataSource_basic(t *testing.T) {
	name := acctest.RandomName("srv")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acctest.CatalogConfig() + serverConfig(serverOpts{Hostname: name, Image: "local.acc_image", Package: "local.acc_package"}) + `
data "pidginhost_server" "by_name" { hostname = pidginhost_server.test.hostname }
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.pidginhost_server.by_name", "id", "pidginhost_server.test", "id"),
				resource.TestCheckResourceAttrPair("data.pidginhost_server.by_name", "ipv4_address", "pidginhost_server.test", "ipv4_address"),
			),
		}},
	})
}

func TestUnitServer_firewallNeedsPublicInterface(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
resource "pidginhost_server" "test" {
  image                   = "debian-12"
  package                 = "cloud-xs"
  no_network_acknowledged = true
  firewall_policy_in      = "DROP"
}
`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`Firewall needs a public interface`),
		}},
	})
}

func TestUnitServer_noNetwork(t *testing.T) {
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.FakeProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: f.ProviderConfig() + `
resource "pidginhost_server" "test" {
  image   = "debian-12"
  package = "cloud-xs"
}
`,
				ExpectError: regexp.MustCompile(`no_network_acknowledged`),
			},
			{
				// A server without a public interface has no firewall settings.
				Config: f.ProviderConfig() + `
resource "pidginhost_server" "test" {
  image                   = "debian-12"
  package                 = "cloud-xs"
  no_network_acknowledged = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("pidginhost_server.test", "ipv4_address"),
					resource.TestCheckNoResourceAttr("pidginhost_server.test", "firewall_policy_in"),
				),
			},
		},
	})
}
