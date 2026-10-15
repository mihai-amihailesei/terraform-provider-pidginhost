// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package provider wires the provider configuration to the resources and
// data sources of each service package.
package provider

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/apiclient"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/account"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/catalog"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/firewall"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/network"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/objectstorage"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/server"
	"github.com/mihai-amihailesei/terraform-provider-pidginhost/internal/service/volume"
)

// Environment variables read by the provider.
const (
	EnvToken  = "PIDGINHOST_TOKEN"
	EnvAPIURL = "PIDGINHOST_API_URL"
)

// Option customises the provider. It exists for tests, which point the
// provider at a fake API and shorten polling.
type Option func(*pidginhostProvider)

// WithPollInterval overrides the interval used to poll asynchronous
// operations.
func WithPollInterval(d time.Duration) Option {
	return func(p *pidginhostProvider) { p.pollInterval = d }
}

// WithHTTPClient injects an HTTP client (used by tests).
func WithHTTPClient(c *http.Client) Option {
	return func(p *pidginhostProvider) { p.httpClient = c }
}

type pidginhostProvider struct {
	version      string
	pollInterval time.Duration
	httpClient   *http.Client
}

var _ provider.Provider = (*pidginhostProvider)(nil)

// New returns a provider factory.
func New(version string, opts ...Option) func() provider.Provider {
	return func() provider.Provider {
		p := &pidginhostProvider{version: version}
		for _, o := range opts {
			o(p)
		}
		return p
	}
}

type providerModel struct {
	Token  types.String `tfsdk:"token"`
	APIURL types.String `tfsdk:"api_url"`
}

func (p *pidginhostProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pidginhost"
	resp.Version = p.version
}

func (p *pidginhostProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Pidgin Host provider manages resources in the [Pidgin Host](https://www.pidginhost.com) cloud: " +
			"servers, private networks, public and floating IPs, firewalls, volumes, object storage buckets and SSH keys.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				MarkdownDescription: "API token, sent as `Authorization: Token <token>`. Create one in the Pidgin Host control panel " +
					"under *Account → API tokens*. Can also be set with the `" + EnvToken + "` environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"api_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the API. Defaults to `" + apiclient.DefaultBaseURL + "`. " +
					"Can also be set with the `" + EnvAPIURL + "` environment variable.",
				Optional: true,
			},
		},
	}
}

func (p *pidginhostProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Unknown API token",
			"The provider cannot be configured with a token that is only known after apply. "+
				"Set it to a static value or use the "+EnvToken+" environment variable.")
	}
	if cfg.APIURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Unknown API URL",
			"The provider cannot be configured with an API URL that is only known after apply.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	token := os.Getenv(EnvToken)
	if !cfg.Token.IsNull() {
		token = cfg.Token.ValueString()
	}
	apiURL := os.Getenv(EnvAPIURL)
	if !cfg.APIURL.IsNull() {
		apiURL = cfg.APIURL.ValueString()
	}

	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token",
			"Set the token argument in the provider block or the "+EnvToken+" environment variable.")
		return
	}

	client, err := apiclient.New(apiclient.Config{
		Token:        token,
		BaseURL:      apiURL,
		UserAgent:    "terraform-provider-pidginhost/" + p.version,
		PollInterval: p.pollInterval,
		HTTPClient:   p.httpClient,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}

	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *pidginhostProvider) Resources(_ context.Context) []func() resource.Resource {
	var all []func() resource.Resource
	all = append(all, server.Resources()...)
	all = append(all, network.Resources()...)
	all = append(all, firewall.Resources()...)
	all = append(all, volume.Resources()...)
	all = append(all, objectstorage.Resources()...)
	all = append(all, account.Resources()...)
	return all
}

func (p *pidginhostProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	var all []func() datasource.DataSource
	all = append(all, server.DataSources()...)
	all = append(all, objectstorage.DataSources()...)
	all = append(all, account.DataSources()...)
	all = append(all, catalog.DataSources()...)
	return all
}
