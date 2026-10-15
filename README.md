# Terraform / OpenTofu provider for Pidgin Host

A Terraform and OpenTofu provider for the [Pidgin Host](https://www.pidginhost.com) cloud, built on
[terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework) and the official
[Pidgin Host Go SDK](https://github.com/pidginhost/sdk-go).

It manages servers, private networks, public and floating IPs, firewalls, volumes, object storage buckets and SSH
keys. Every resource supports import, configurable timeouts and drift detection.

> **Status: pre-release (v0.1).** Every resource has unit tests against an in-memory model of the API and
> acceptance tests for the real API. Until the acceptance suite has been run against a real account, treat it as
> beta.

## Relationship to the official provider

Pidgin Host publishes an official provider,
[`pidginhost/pidginhost`](https://github.com/pidginhost/terraform-provider-pidginhost), under the MIT licence. It
provides a single `pidginhost_server` resource.

This provider is an **independent implementation**. It shares no code with the official provider, and it is not
affiliated with or endorsed by Pidgin Host. It covers more of the API (see the table below), uses the generated SDK
rather than a hand-written HTTP client, and is licensed under the [MPL-2.0](LICENSE), the usual licence for
Terraform providers. Both providers use the type prefix `pidginhost_`, so do not use them in the same configuration.

## Resources and data sources

| Resource | Purpose |
| --- | --- |
| `pidginhost_server` | Cloud server. Async creation with status polling; in-place package change, power state, destroy protection, firewall set |
| `pidginhost_private_network` | Private network (CIDR, gateway) |
| `pidginhost_private_network_attachment` | Server membership in a private network |
| `pidginhost_ipv4`, `pidginhost_ipv6` | Public address with reverse DNS |
| `pidginhost_ipv4_attachment`, `pidginhost_ipv6_attachment` | Public address attached to a server |
| `pidginhost_floating_ipv4`, `pidginhost_floating_ipv6` | Floating address with label and reverse DNS |
| `pidginhost_floating_ip_authorization` | Allows a server to use a floating address |
| `pidginhost_firewall_rules_set` | Firewall rule set |
| `pidginhost_firewall_rule` | One rule in a set (separate, so rule changes never recreate the set) |
| `pidginhost_volume` | Block volume; online grow, move between servers |
| `pidginhost_volume_attachment` | Attaches a volume not managed by `pidginhost_volume` |
| `pidginhost_bucket` | S3-compatible bucket; async creation, in-place quota and visibility |
| `pidginhost_bucket_credentials` | A bucket's access keys, with rotation |
| `pidginhost_ssh_key` | Account SSH key |

Data sources: `pidginhost_images`, `pidginhost_server_packages`, `pidginhost_storage_products`,
`pidginhost_server`, `pidginhost_ssh_key`, `pidginhost_bucket`.

Full documentation is in [`docs/`](docs/), generated from the schemas and [`examples/`](examples/). For a full stack
(two servers on a private network, firewall, floating IP, volume, bucket with credentials), see
[`examples/complete`](examples/complete).

## Requirements

- Terraform **1.6** or later, or OpenTofu **1.8** or later. CI tests Terraform 1.6, 1.9 and latest, and OpenTofu
  1.8 and latest.
- A Pidgin Host API token.
- To build from source: Go (the version in [`go.mod`](go.mod)).

## Installation

### From a registry

Once published, declare the provider and run `terraform init` or `tofu init`:

```hcl
terraform {
  required_providers {
    pidginhost = {
      source  = "mihai-amihailesei/pidginhost"
      version = "~> 0.1"
    }
  }
}

provider "pidginhost" {}   # token from PIDGINHOST_TOKEN
```

The same `source` works for both CLIs: Terraform resolves it on `registry.terraform.io` and OpenTofu on
`registry.opentofu.org`. If the provider is published under a different namespace, replace `mihai-amihailesei`
with it.

### From source, with a development override

Build and install the binary into `$(go env GOPATH)/bin`:

```sh
make install
```

Then tell the CLI to use that binary instead of downloading the provider.

**Terraform:** create `~/.terraformrc` (on Windows, `%APPDATA%\terraform.rc`), or point
`TF_CLI_CONFIG_FILE` at a file of your choice:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/mihai-amihailesei/pidginhost" = "/home/you/go/bin"   # output of: go env GOPATH, plus /bin
  }
  direct {}
}
```

**OpenTofu:** create `~/.tofurc` (on Windows, `%APPDATA%\tofu.rc`), or point `TF_CLI_CONFIG_FILE` at a file:

```hcl
provider_installation {
  dev_overrides {
    "registry.opentofu.org/mihai-amihailesei/pidginhost" = "/home/you/go/bin"
  }
  direct {}
}
```

With an override in place, skip `init` for this provider: run `plan` or `apply` directly. The CLI prints a
warning that development overrides are active.

## Authentication

```sh
export PIDGINHOST_TOKEN="..."   # sent as "Authorization: Token <token>"
```

| Provider argument | Environment variable | Default |
| --- | --- | --- |
| `token` (sensitive) | `PIDGINHOST_TOKEN` | — (required) |
| `api_url` | `PIDGINHOST_API_URL` | `https://www.pidginhost.com` |

Never commit a token to version control. Server passwords and bucket credentials are stored in the state, so use
an encrypted, access-controlled state backend (or OpenTofu state encryption).

## Design notes

- **Asynchronous operations** (server and bucket creation, package changes, deletions) are polled until they
  finish or the `timeouts` value expires. The resource ID is saved as soon as the API accepts a create request, so
  a server or bucket that fails to provision is tainted and replaced, rather than orphaned and still billed.
- **Plan modifiers.** Attributes force replacement only where the API has no way to update them. Attributes that
  can be set only at creation and that the API never returns (`user_data`, `password`, `public_ipv4`, …) can be
  added to the configuration after `terraform import` without forcing a replacement.
- **SDK workarounds** live only in [`internal/apiclient/sdk_workarounds.go`](internal/apiclient/sdk_workarounds.go).
  Every API or SDK problem found while building the provider is listed in
  [`UPSTREAM_ISSUES.md`](UPSTREAM_ISSUES.md).
- **Layout.** Each API area is a package under `internal/service/` exposing `Resources()` and `DataSources()`. To
  add Kubernetes, DNS or another area, create a package there and register it in
  [`internal/provider/provider.go`](internal/provider/provider.go).

## Development

```sh
make build        # bin/terraform-provider-pidginhost
make test         # unit tests; needs terraform or tofu on PATH, no credentials
make lint         # golangci-lint
make docs         # regenerate docs/ with tfplugindocs (commit the result)
```

### Unit tests

`make test` needs no credentials. Besides schema and helper tests, every resource is tested through full
plan/apply/import/destroy cycles. These run against [`internal/fakeapi`](internal/fakeapi), an in-memory, stateful
model of the API whose responses follow the OpenAPI spec. It includes asynchronous provisioning, out-of-band
changes (drift) and error responses. The tests use the `terraform` (or `tofu`) binary on `PATH`. Set
`TF_ACC_TERRAFORM_PATH` to choose one; for OpenTofu also set `TF_ACC_PROVIDER_HOST=registry.opentofu.org`.

### Acceptance tests

Acceptance tests create **real, billable resources**. They need `PIDGINHOST_TOKEN`, and the make targets set
`TF_ACC=1`.

```sh
export PIDGINHOST_TOKEN=...
make testacc-ssh-key              # one resource at a time
make testacc-server
make testacc                      # everything
make sweep                        # delete leftovers from failed runs
```

`make testacc-fake` runs the whole acceptance suite against the in-memory fake API instead. It is free and needs no
token. It checks that the acceptance tests themselves are sound, but proves nothing about the real API.

There is one `make testacc-<resource>` target per resource and data source; see the end of the
[`Makefile`](Makefile).

To keep costs down, the tests use the package with the least memory, the smallest volume the storage product
allows, and 1 GB buckets. They
destroy everything they create. Every resource they create is named with the prefix **`tfacc-`**, and the sweepers
delete **only** resources with that prefix: hostnames, aliases, bucket names, labels, or reverse DNS names for
public IPs. Private networks have no name field, so the tests use networks in `10.213.0.0/16`, and the sweeper
reports leftovers there without deleting them.

These environment variables tune the acceptance tests:

| Variable | Purpose |
| --- | --- |
| `PIDGINHOST_ACC_IMAGE` | OS image slug (default: the first Debian or Ubuntu image) |
| `PIDGINHOST_ACC_PACKAGE` | Server package slug (default: the package with the least memory) |
| `PIDGINHOST_ACC_PACKAGE_UPGRADE` | Package for the in-place resize test (default: the second smallest) |
| `PIDGINHOST_ACC_STORAGE_PRODUCT` | Storage product slug for volumes (default: the first product) |
| `PIDGINHOST_ACC_RDNS_DOMAIN` | Domain for reverse DNS names in tests (default: `example.com`) |

The **Acceptance tests** GitHub workflow runs the same targets on manual dispatch, using a `PIDGINHOST_TOKEN`
repository secret, and always runs the sweeper afterwards.

## Releasing

Releases are built by GoReleaser ([`.goreleaser.yml`](.goreleaser.yml)) in the layout both registries require:
per-platform zips, `SHA256SUMS`, a detached GPG signature of the checksums, and the registry manifest.

1. Add an ASCII-armored GPG private key and its passphrase as the repository secrets `GPG_PRIVATE_KEY` and
   `PASSPHRASE`.
2. Push a tag such as `v0.1.0`. The **Release** workflow creates a draft GitHub release; review it, then publish.
3. Publish the provider on the registries:
   - **Terraform Registry:** the repository must be public and named `terraform-provider-pidginhost`. Sign in to
     registry.terraform.io with GitHub, add the GPG public key, and publish the repository.
   - **OpenTofu Registry:** submit the provider and the GPG public key through the
     [opentofu/registry](https://github.com/opentofu/registry) repository's issue templates.

If the provider moves to another owner (for example, if it is adopted by Pidgin Host), update the Go module path in
`go.mod`, the `registryAddress` in `main.go`, and the `source` in the examples.

## Licence

[MPL-2.0](LICENSE). The Pidgin Host Go SDK is a dependency; its licensing status is tracked in
[`UPSTREAM_ISSUES.md`](UPSTREAM_ISSUES.md#a1-sdk-go-has-no-licence-file).
