// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/mihai-amihailesei/pidgin-host/internal/provider"
)

// TestProviderSchemas checks that the provider and every resource and data
// source schema is valid and that every type uses the pidginhost_ prefix.
func TestProviderSchemas(t *testing.T) {
	srv, err := providerserver.NewProtocol6WithError(provider.New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("%s: %s", d.Summary, d.Detail)
	}
	for name := range resp.ResourceSchemas {
		if !strings.HasPrefix(name, "pidginhost_") {
			t.Errorf("resource %s lacks the pidginhost_ prefix", name)
		}
	}
	for name := range resp.DataSourceSchemas {
		if !strings.HasPrefix(name, "pidginhost_") {
			t.Errorf("data source %s lacks the pidginhost_ prefix", name)
		}
	}
	tokenAttr := findAttr(resp.Provider.Block, "token")
	if tokenAttr == nil || !tokenAttr.Sensitive {
		t.Error("provider token must exist and be sensitive")
	}
}

func findAttr(b *tfprotov6.SchemaBlock, name string) *tfprotov6.SchemaAttribute {
	for _, a := range b.Attributes {
		if a.Name == name {
			return a
		}
	}
	return nil
}
