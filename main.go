// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/mihai-amihailesei/pidgin-host/internal/provider"
)

//go:generate go tool tfplugindocs generate --provider-name pidginhost --rendered-provider-name "Pidgin Host"

// version is set by GoReleaser at build time.
var version = "dev"

// registryAddress is the provider's source address. It only matters when the
// provider runs in debug mode; installed providers are addressed through the
// required_providers block of the configuration.
var registryAddress = "registry.terraform.io/mihai-amihailesei/pidginhost"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "start the provider in debug mode, for use with delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: registryAddress,
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
