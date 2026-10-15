// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

// Package apiclient builds the Pidgin Host SDK client used by every resource
// and data source, and holds the helpers they share: error classification,
// polling and pagination.
//
// It deliberately contains no HTTP client of its own. Every API call goes
// through github.com/pidginhost/sdk-go. The only exceptions are the SDK
// workarounds in sdk_workarounds.go, each of which is listed in
// UPSTREAM_ISSUES.md.
package apiclient

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/pidginhost/sdk-go"
)

// DefaultBaseURL is the production API endpoint.
const DefaultBaseURL = "https://www.pidginhost.com"

// DefaultPollInterval is how often asynchronous operations are polled.
const DefaultPollInterval = 5 * time.Second

// Config holds what is needed to build a Client.
type Config struct {
	Token     string
	BaseURL   string
	UserAgent string

	// PollInterval overrides DefaultPollInterval. Tests set it low.
	PollInterval time.Duration

	// MaxRetries is the number of retries for throttled (429) requests and
	// for transient failures of idempotent requests. Zero means the default.
	MaxRetries int

	// HTTPClient lets tests inject a transport. When nil a default client
	// is used.
	HTTPClient *http.Client
}

// Client wraps the generated SDK client.
type Client struct {
	SDK          *sdk.APIClient
	PollInterval time.Duration
}

// Cloud returns the /api/cloud/ service.
func (c *Client) Cloud() *sdk.CloudAPIService { return c.SDK.CloudAPI }

// Account returns the /api/account/ service.
func (c *Client) Account() *sdk.AccountAPIService { return c.SDK.AccountAPI }

// New builds a Client from cfg.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("an API token is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, fmt.Errorf("api_url %q must start with https:// or http://", cfg.BaseURL)
	}

	retries := cfg.MaxRetries
	if retries <= 0 {
		retries = 4
	}

	base := http.DefaultTransport
	timeout := 2 * time.Minute
	if cfg.HTTPClient != nil {
		if cfg.HTTPClient.Transport != nil {
			base = cfg.HTTPClient.Transport
		}
		if cfg.HTTPClient.Timeout != 0 {
			timeout = cfg.HTTPClient.Timeout
		}
	}

	sdkCfg := sdk.NewConfiguration()
	sdkCfg.Servers = sdk.ServerConfigurations{{URL: baseURL, Description: "configured"}}
	sdkCfg.AddDefaultHeader("Authorization", "Token "+cfg.Token)
	if cfg.UserAgent != "" {
		sdkCfg.UserAgent = cfg.UserAgent
	}
	sdkCfg.HTTPClient = &http.Client{
		Timeout:   timeout,
		Transport: newLoggingTransport(newRetryTransport(base, retries)),
	}

	interval := cfg.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	return &Client{SDK: sdk.NewAPIClient(sdkCfg), PollInterval: interval}, nil
}
