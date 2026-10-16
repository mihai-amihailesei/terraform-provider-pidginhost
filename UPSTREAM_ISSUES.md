# Upstream issues: Pidgin Host API and Go SDK

Problems found while building this provider against the Pidgin Host API
(OpenAPI schema `1.0.0 (v1)`, as published at
`https://www.pidginhost.com/api/schema/` on 2026-10-15) and
[`github.com/pidginhost/sdk-go`](https://github.com/pidginhost/sdk-go) v0.14.0
(commit `b973ac9`, the latest tag on that date).

Each entry is written to be filed as-is. Entries are grouped by severity:

- **A — Blocking or data-affecting.** The provider needs a workaround, or
  users can hit failures or data loss.
- **B — Missing capability.** Something the API cannot do, which forces
  replacements or awkward resource designs.
- **C — Spec gap.** Behaviour the provider had to assume. Every place this
  applies is marked with an `Assumption:` comment in the code.
- **D — Cosmetic.** Inconsistencies and documentation problems.

When an entry is fixed upstream, delete it here and remove the
corresponding workaround or assumption in the code.

---

## A. Blocking or data-affecting

### A1. sdk-go has no licence file

`pidginhost/sdk-go` has no `LICENSE`, `COPYING` or `NOTICE` file, either at
tag v0.14.0 or on `main`, and the README does not state a licence. The
bundled `api/openapi.yaml` says `license: MIT` in its `info` block, but that
describes the API description, not the SDK code.

Without a licence, nobody else may legally redistribute the SDK. Every
compiled program that uses it, including Terraform provider binaries,
redistributes it.

**Request:** add a `LICENSE` file to `pidginhost/sdk-go`. MIT would match the
official Terraform provider and the API schema. Tag a release that includes
it.

### A2. `Bucket.used_bytes` and `Bucket.object_count` are int32 in the SDK

The current schema declares both as `type: integer, format: int64`. sdk-go
v0.14.0 was generated from a schema without the `format`, so it declares
them `int32`. Decoding any bucket that holds more than 2 147 483 647 bytes
(2 GiB), or more objects than that, fails:

```
json: cannot unmarshal number 5368709120 into Go struct field Bucket.used_bytes of type int32
```

This affects `CloudBucketsRetrieve`, `CloudBucketsList`,
`CloudBucketsCreate`, `CloudBucketsResizeCreate` and
`CloudBucketsVisibilityCreate`.

**Request:** regenerate the SDK from the current schema and release it.

*Provider workaround:* `internal/apiclient/sdk_workarounds.go` decodes bucket
responses again into a type with int64 counters.
`TestGetBucketLargeCounters` reports when the SDK no longer needs it.

### A3. sdk-go lags behind the published schema

Beyond A2, the schema now has properties the SDK does not: `billing` on
`Server`, `ServerDetail`, `Volume`, `PrivateNetwork`, `Bucket`,
`DedicatedServer`, `HostingService` and `Service`. There are also new
schemas (`ResourceBilling`, `Dashboard*`, `HostingUsage*`, `TLDCatalog`,
`DomainAvailability`, `InvoiceLocalAmounts`, …) and changed fields on
`ServerTrafficResponse`, `TLD`, `InvoiceList` and `InvoiceDetail`. Unknown
properties are ignored when decoding, so `billing` causes no errors today.

**Request:** regenerate and release sdk-go whenever the schema changes, for
example from a CI job that diffs the published schema.

### A4. Generated models fail hard on a missing required property or an unknown enum value

Every generated model's `UnmarshalJSON` rejects a response that lacks any
property the schema lists as `required`. Enum types (such as
`ResourceStatusEnum` and `FwPolicyOutEnum`) reject values not in the enum. So
any of the following breaks every client:

- the API omitting a field, for example a server without a `networks.private`
  list, or `boot_iso` on an older record;
- the API adding a status, such as a new server lifecycle state.

**Request:**

1. Make sure the API always returns every property the schema marks
   `required` (or stop marking them required).
2. Generate the SDK with `enumUnknownDefaultCase=true`, so a new enum value
   decodes as "unknown" instead of failing.

### A5. Deleting a server: fate of addresses allocated with `new_ipv4` / `new_ipv6` is undocumented

`ServerAddRequest.new_ipv4` and `new_ipv6` allocate an address that the
caller never received an ID for. The spec does not say whether
`DELETE /api/cloud/servers/{id}/` releases these addresses or leaves them
allocated (and billed) but unattached.

**Request:** document the behaviour. Preferably release such addresses with
the server, or return their IDs in the create response.

---

## B. Missing capabilities

### B1. No endpoint to create a standalone volume

Volumes have `GET/PUT/PATCH/DELETE /api/cloud/volumes/{id}/` and
`attach`/`detach` actions, but they can only be created through
`POST /api/cloud/servers/{server_id}/volumes/`, which attaches them at once.
There is no `POST /api/cloud/volumes/`. As a result:

- A volume cannot be created before the server that will use it.
- The provider's `pidginhost_volume` resource must require a `server_id`.
  That overlaps with `pidginhost_volume_attachment` instead of separating
  "the disk" from "where it is attached".

**Request:** add `POST /api/cloud/volumes/` taking `product`, `size`,
`alias` and `project`.

### B2. A floating IP's label cannot be changed

`/api/cloud/floating-ipv4/{id}/` and `/api/cloud/floating-ipv6/{id}/` have no
`PUT` or `PATCH`. Changing a label means deleting and recreating the floating
IP, which **changes its address**. That defeats the point of a floating IP.

**Request:** add `PATCH` for `label` on both endpoints.

### B3. A server cannot be renamed, and `Server` and `ServerDetail` disagree on whether hostname is writable

`ServerDetail.hostname` is `readOnly`, and neither `ServerDetailRequest` nor
`PatchedServerDetailRequest` accepts it. However, the `Server` schema (used in
the list response) declares `hostname` writable (`maxLength: 200`, no
`readOnly`). It also has a different maximum length from
`ServerAddRequest.hostname` (`maxLength: 253`).

**Request:** allow renaming through `PATCH /api/cloud/servers/{id}/`, or mark
`Server.hostname` read-only, and use one maximum length.

### B4. High availability cannot be changed after creation

`ServerAddRequest.enable_ha` sets HA at creation. `ServerDetail.ha_enabled`
is read-only, and there is no action to change it, so turning HA on or off
means recreating the server.

**Request:** an action, or a writable `ha_enabled` field, to toggle HA on an
existing server.

### B5. Private network membership is keyed by hostname, not server ID

`POST /api/cloud/private-networks/{id}/add-server/` takes
`{"server": "<hostname>"}`, and `remove-server/` takes a hostname or private
IP. Every other server reference in the API is an ID. Hostnames are not
guaranteed unique: `ServerAddRequest.hostname` is optional and nothing says
it must be unique within an account. Two servers with the same hostname would
make these calls ambiguous.

**Request:** accept a server ID (`server_id`) on both actions.

### B6. `Volume` reports its server by hostname only

`Volume.server` is the hostname of the attached server, and there is no
`server_id`. sdk-go v0.14.0 added `server_id` to `PublicIPv4` and
`PublicIPv6` for exactly this reason. `Volume` needs the same, as
`AttachVolumeRequest.vm` already takes an ID.

**Request:** add `server_id` (nullable integer) to `Volume`.

### B7. A reverse DNS name cannot be removed

`ReverseDNSRequest.reverse_dns` has `minLength: 1`, and there is no `DELETE`
on the `rdns/` endpoints. A PTR record, once set, can only be replaced, never
reset to the default.

**Request:** accept an empty value, or add `DELETE`, to restore the default
PTR record.

### B8. `detach-ipv6` cannot target a specific address

`POST /api/cloud/servers/{id}/detach-ipv4/` takes an optional `ipv4` query
parameter to pick the address. `detach-ipv6/` has no equivalent and always
detaches "the" IPv6. (The provider uses `/api/cloud/ipv6/{id}/detach/`
instead, which names the address.)

**Request:** add an `ipv6` query parameter to `detach-ipv6/`, matching
`detach-ipv4/`.

---

## C. Behaviour the spec does not define

These are the places where the provider had to assume behaviour. Each is
marked with an `Assumption:` comment in the code. Please document the actual
behaviour in the schema descriptions.

### C1. Server lifecycle and status transitions

`POST /api/cloud/servers/` answers `201` with only `{id}`, and the server is
provisioned asynchronously. `ResourceStatusEnum` lists twelve statuses, but
not which transitions occur.

The provider treats these as transitional while waiting for `active`:
`pending`, `provisioning`, `upgrading`, `resuming`. It treats these as
failures: `failed`, `suspending`, `suspended`, `cancelling`, `cancelled`,
`terminating`, `terminated`.

**Request:** document the state machine. Returning the full `ServerDetail`
from the create call would also save a request.

### C2. `modify-package` preconditions and completion

`POST .../modify-package/` answers `{upgrading: bool}`. The spec does not say:

- whether the server must be stopped first;
- whether disks are resized;
- whether a downgrade is allowed;
- how long the change takes;
- how completion is reported.

The provider waits for the status to return to `active`, then checks the
package.

### C3. Format of `ServerDetail.package`, `ServerDetail.image` and `Volume.product`

The create and update calls accept "ID or slug", but the responses' format is
not documented. It could be the slug, the ID or the display name. The
provider compares packages through the catalog to avoid perpetual diffs. It
keeps the configured image and product, because they cannot change.

**Request:** document what these fields return. Ideally return both the ID
and the slug.

### C4. Power state values

`PowerManagement.status` is a free-form string with no enum, and
`ServerDetail.machine` is a free-form object. The provider assumes that
`running` means powered on, and that `stopped` / `shutoff` / `off` mean
powered off.

**Request:** an enum for `PowerManagement.status`, and a typed schema for
`machine`.

### C5. Public interface firewall fields

- `PublicInterface.fw_rules_set` is documented as "ID or slug", but
  `FirewallRulesSet` has no `slug` property. In practice the GET can return
  neither: for a free-tier server it returned `"free tier fw"`, the name of
  the platform's read-only set (`GET /api/cloud/firewall-rules-set/` lists it
  as `{"id": 767, "name": "Free tier FW", "read_only": true}`) in lower case.
  A lower-cased name is not unique, so a client cannot always map it back to
  a set. Please return the ID (and document the field as an ID).
- The spec does not mention that the platform attaches a read-only rule set
  (and an inbound `DROP` policy) to every free-tier server, nor whether such
  a server accepts a user rule set in its place, or detaching the read-only
  one.
- Detaching a rule set requires sending `"fw_rules_set": null` explicitly.
  Omitting the field leaves the set attached. This caused the official
  Terraform provider's "inconsistent result after apply" bug. Please state it
  in the description.
- What `DELETE /api/cloud/servers/{id}/public-interface/` does is not
  described: does it remove the firewall, or the interface and its IPs?

*Provider workaround:* `firewall_rules_set_id` is optional and computed, so a
set the platform attached is recorded without a diff and never detached
unless the configuration set the attribute. A name returned by the GET is
resolved to an ID through the rule set list, ignoring case and including
read-only sets (`resolveRulesSetID` in `internal/service/server/api.go`).

### C6. Asynchrony of attach/detach and resize actions

The spec does not say whether these take effect before the call returns:

- volume `attach` and `detach`;
- private network `add-server` and `remove-server`;
- volume resize (`PATCH /volumes/{id}/` with a larger `size`);
- bucket `resize` and `visibility`.

The provider polls until it sees the change.

### C7. What `PrivateNetwork.provisioned` means

`provisioned` is exported, but nothing says when it becomes `true`: on
creation, or only once a server joins. Nor does it say whether servers can be
attached before then.

### C8. Error responses for "resource in use"

The spec lists only `204` for these deletes, with no error responses:

- `DELETE` of a firewall rule set still applied to a server;
- `DELETE` of an attached public IP;
- `DELETE` of an attached volume.

The provider detaches first, and for rule sets treats `400` / `409` as "in
use".

**Request:** document the error status and body. More generally, add error
response schemas (`400`, `403`, `404`, `409`, `429`) to every operation.

### C9. Firewall rule fields and validation

- `FirewallRule.protocol` is a free-form string (max 100), while a
  `ProtocolEnum` (`tcp`, `udp`) exists elsewhere in the schema. Which values
  are accepted (`icmp`? empty for "any")?
- `has_error` and `error_message` appear on rules and the set has a
  `pending` status, but it is not documented whether validation is
  synchronous.

### C10. `user_data` format

`user_data` is documented as "an executable script with a shebang". Please
say whether cloud-init's `#cloud-config` format is accepted.

### C11. Bucket naming rules

`BucketCreateRequest.name` only has `maxLength: 63`. The accepted characters,
global versus per-account uniqueness, and how `full_name` is derived are not
documented.

### C12. Rate limiting

Rate limits are not documented. The provider retries `429` responses and
honours `Retry-After`, assuming the API sends it.

---

## D. Inconsistencies and documentation problems

### D1. Internal docstrings leak into the public schema

Several operation and schema descriptions are Python docstrings meant for
developers, not API users, for example:

- `GET /api/cloud/buckets/` and the firewall rule set operations:
  *"Adds :class:\`~account.iam_enforcement.IAMActionPermission\` as an
  intersection with the route's existing permission classes (spec §6)…"*;
- `BucketCancelResponse`: *"Body of the 202 answered by
  ``BucketViewSet.destroy``…"*;
- every `/api/account/ssh-keys/` operation: *"Account context + IAM role
  enforcement for the account residue…"*.

Many operations instead have only a generic description ("Cloud servers",
"Public interface").

### D2. Inconsistent pagination

Most list endpoints are paginated (`count` / `next` / `previous` /
`results`), but these return a plain array: `GET /api/cloud/buckets/`,
`/api/cloud/volumes/`, `/api/cloud/firewall-rules-set/`,
`/api/cloud/firewall-rules-set/{id}/rules/` and
`/api/cloud/servers/{id}/volumes/`.

### D3. Inconsistent path parameter types

Most `{id}` path parameters are integers, but the firewall rule endpoints
(`{rules_set_id}`, `{rule_id}`), the server volume endpoints (`{server_id}`,
`{volume_id}`) and `/api/account/ssh-keys/{id}/` declare theirs as strings.
The SDK mirrors this (`int32` in some methods, `string` in others).

### D4. `GET /api/cloud/server-packages/by-generation/` is typed as a single object

It is described as "List of available server products", has no parameters
(not even `generation`), and its response is a single `ServerProduct`. It is
presumably meant to be a grouped list, or to take a `generation` parameter.
(The provider uses `GET /api/cloud/server-packages/?generation=` instead.)

### D5. Units are not stated

`ServerProduct.memory`, `ServerProduct.disk_size`, `ServerDetail.memory` and
`ServerDetail.disk_size` have no unit (MB? GB? GiB?). `Volume.size` says
"GB" and `ServerProduct.traffic` says "TB", so the others should too.
