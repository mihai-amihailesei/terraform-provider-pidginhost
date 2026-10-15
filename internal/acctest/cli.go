// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6/tf6server"

	"github.com/mihai-amihailesei/pidgin-host/internal/provider"
)

// CLIPath returns the terraform or tofu binary to use for CLI-driven tests:
// TF_ACC_TERRAFORM_PATH if set, else terraform or tofu from PATH.
func CLIPath() string {
	if p := os.Getenv("TF_ACC_TERRAFORM_PATH"); p != "" {
		return p
	}
	for _, name := range []string{"terraform", "tofu"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// CLI is a terraform/tofu working directory whose provider
// "mihai-amihailesei/pidginhost" is served in-process by this provider build.
// Pass Reattach to every command.
type CLI struct {
	*tfexec.Terraform
	Reattach *tfexec.ReattachOption
}

// NewCLI copies the .tf files of dir into a temporary directory and returns
// a CLI for it, so configurations are tested exactly as users write them,
// required_providers block included. vars are written to
// terraform.tfvars.json. The test is skipped when no CLI is available.
func NewCLI(t *testing.T, dir string, vars map[string]any, opts ...provider.Option) *CLI {
	t.Helper()
	cli := CLIPath()
	if cli == "" {
		t.Skip("no terraform or tofu binary found; set TF_ACC_TERRAFORM_PATH")
	}

	work := t.TempDir()
	files, _ := filepath.Glob(filepath.Join(dir, "*.tf"))
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, filepath.Base(file)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if len(vars) > 0 {
		b, err := json.Marshal(vars)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "terraform.tfvars.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	host := "registry.terraform.io"
	if strings.Contains(filepath.Base(cli), "tofu") {
		host = "registry.opentofu.org"
	}
	addr := host + "/mihai-amihailesei/pidginhost"

	// The in-process provider would otherwise log every RPC at trace level.
	if os.Getenv("TF_LOG") == "" {
		for _, k := range []string{"TF_LOG_PROVIDER", "TF_LOG_SDK", "TF_LOG_SDK_PROTO", "TF_LOG_SDK_FRAMEWORK"} {
			t.Setenv(k, "OFF")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	reattachCh := make(chan *plugin.ReattachConfig)
	closeCh := make(chan struct{})
	go func() {
		err := tf6server.Serve(addr, providerserver.NewProtocol6(provider.New("test", opts...)()),
			tf6server.WithDebug(ctx, reattachCh, closeCh), tf6server.WithoutLogStderrOverride())
		if err != nil {
			t.Errorf("serving provider: %v", err)
		}
	}()
	var rc *plugin.ReattachConfig
	select {
	case rc = <-reattachCh:
	case <-time.After(30 * time.Second):
		t.Fatal("provider did not start")
	}
	t.Cleanup(func() {
		cancel()
		<-closeCh
	})

	reattach := tfexec.Reattach(tfexec.ReattachInfo{
		addr: {
			Protocol: string(rc.Protocol), ProtocolVersion: rc.ProtocolVersion, Pid: rc.Pid, Test: true,
			Addr: tfexec.ReattachConfigAddr{Network: rc.Addr.Network(), String: rc.Addr.String()},
		},
	})

	tf, err := tfexec.NewTerraform(work, cli)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CHECKPOINT_DISABLE": "1"}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "PIDGINHOST_") ||
			k == "PATH" || k == "HOME" || k == "HTTPS_PROXY" || k == "HTTP_PROXY" || k == "NO_PROXY" || k == "SSL_CERT_FILE" {
			env[k] = v
		}
	}
	if err := tf.SetEnv(env); err != nil {
		t.Fatal(err)
	}
	return &CLI{Terraform: tf, Reattach: reattach}
}
