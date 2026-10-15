// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package provider_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/acctest"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/fakeapi"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/provider"
)

// TestUnitExampleComplete applies examples/complete, unchanged, against the
// fake API with the real CLI; checks that a second plan is empty; and
// destroys everything.
func TestUnitExampleComplete(t *testing.T) {
	f := acctest.NewFake(t)
	t.Setenv("PIDGINHOST_TOKEN", fakeapi.Token)
	t.Setenv("PIDGINHOST_API_URL", f.URL)

	tf := acctest.NewCLI(t, filepath.Join("..", "..", "examples", "complete"),
		map[string]any{"ssh_public_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyForTests example@invalid"},
		provider.WithPollInterval(10*time.Millisecond))
	ctx := context.Background()

	if err := tf.Init(ctx, tf.Reattach); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := tf.Apply(ctx, tf.Reattach); err != nil {
		t.Fatalf("apply: %v", err)
	}
	changes, err := tf.Plan(ctx, tf.Reattach)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if changes {
		t.Error("plan after apply is not empty")
	}

	out, err := tf.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server_ipv4", "server_private_ip", "floating_ip", "bucket", "bucket_secret_key"} {
		if _, ok := out[name]; !ok {
			t.Errorf("output %q missing", name)
		}
	}
	if !out["bucket_secret_key"].Sensitive {
		t.Error("bucket_secret_key output must be sensitive")
	}
	if n := len(f.ServerIDs()); n != 2 {
		t.Errorf("%d servers created, want 2", n)
	}

	if err := tf.Destroy(ctx, tf.Reattach); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if n := len(f.ServerIDs()); n != 0 {
		t.Errorf("%d servers left after destroy", n)
	}
	if n := len(f.BucketIDs()); n != 0 {
		t.Errorf("%d buckets left after destroy", n)
	}
}
