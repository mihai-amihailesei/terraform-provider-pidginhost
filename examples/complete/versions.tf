terraform {
  # Works with Terraform >= 1.6 and OpenTofu >= 1.8.
  required_version = ">= 1.6"

  required_providers {
    pidginhost = {
      source = "mihai-amihailesei/pidginhost"
    }
  }
}

# Reads the API token from the PIDGINHOST_TOKEN environment variable.
provider "pidginhost" {}
