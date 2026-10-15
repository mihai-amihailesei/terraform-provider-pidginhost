# Development tasks for terraform-provider-pidginhost.
#
# Unit tests need no credentials. Acceptance tests (testacc*) create real,
# billable resources and need TF_ACC=1 (set by these targets) and
# PIDGINHOST_TOKEN in the environment.

BINARY      := terraform-provider-pidginhost
ACC_TIMEOUT ?= 120m
TESTARGS    ?=

.PHONY: default build install fmt lint test testacc check-token sweep docs docs-check tidy

default: build

build:
	go build -o bin/$(BINARY) .

# Installs into $(go env GOPATH)/bin, which the dev_overrides block in the
# README points at.
install:
	go install .

fmt:
	gofmt -s -w .
	terraform fmt -recursive examples/ 2>/dev/null || tofu fmt -recursive examples/

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

# Unit tests: schema and plan logic, plus full plan/apply/import cycles
# against the in-memory fake API (internal/fakeapi). Needs a terraform or
# tofu binary on PATH (set TF_ACC_TERRAFORM_PATH to choose one).
test:
	go test ./... -count=1 -timeout 15m $(TESTARGS)

# Every acceptance test. Costs money.
testacc: check-token
	TF_ACC=1 go test ./internal/... -count=1 -v -run '^TestAcc' -timeout $(ACC_TIMEOUT) $(TESTARGS)

check-token:
	@test -n "$$PIDGINHOST_TOKEN" || (echo "PIDGINHOST_TOKEN is not set" >&2; exit 1)

# Deletes leftover resources whose names start with tfacc-.
sweep: check-token
	go test ./internal/sweep -count=1 -v -sweep=all -timeout 60m $(TESTARGS)

docs:
	go generate ./...

# Fails when docs/ is out of date with the schemas and examples.
docs-check: docs
	@git diff --exit-code -- docs/ || (echo "docs/ is out of date: run 'make docs' and commit the result" >&2; exit 1)

# One acceptance-test target per resource, so they can be run one at a time:
#   make testacc-server
define acc_target
.PHONY: testacc-$(1)
testacc-$(1): check-token
	TF_ACC=1 go test ./internal/service/$(2) -count=1 -v -run '$(3)' -timeout $(ACC_TIMEOUT) $(TESTARGS)
endef

$(eval $(call acc_target,server,server,^TestAccServer_))
$(eval $(call acc_target,server-data-source,server,^TestAccServerDataSource_))
$(eval $(call acc_target,private-network,network,^TestAccPrivateNetwork_))
$(eval $(call acc_target,private-network-attachment,network,^TestAccPrivateNetworkAttachment_))
$(eval $(call acc_target,ipv4,network,^TestAccIPv4_))
$(eval $(call acc_target,ipv6,network,^TestAccIPv6_))
$(eval $(call acc_target,ipv4-attachment,network,^TestAccIPv4Attachment_))
$(eval $(call acc_target,ipv6-attachment,network,^TestAccIPv6Attachment_))
$(eval $(call acc_target,floating-ipv4,network,^TestAccFloatingIPv4_))
$(eval $(call acc_target,floating-ipv6,network,^TestAccFloatingIPv6_))
$(eval $(call acc_target,floating-ip-authorization,network,^TestAccFloatingIPAuthorization_))
$(eval $(call acc_target,firewall-rules-set,firewall,^TestAccFirewallRulesSet_))
$(eval $(call acc_target,firewall-rule,firewall,^TestAccFirewallRule_))
$(eval $(call acc_target,volume,volume,^TestAccVolume_))
$(eval $(call acc_target,volume-attachment,volume,^TestAccVolumeAttachment_))
$(eval $(call acc_target,bucket,objectstorage,^TestAccBucket_))
$(eval $(call acc_target,bucket-data-source,objectstorage,^TestAccBucketDataSource_))
$(eval $(call acc_target,bucket-credentials,objectstorage,^TestAccBucketCredentials_))
$(eval $(call acc_target,ssh-key,account,^TestAccSSHKey_))
$(eval $(call acc_target,ssh-key-data-source,account,^TestAccSSHKeyDataSource_))
$(eval $(call acc_target,catalog,catalog,^TestAcc))
