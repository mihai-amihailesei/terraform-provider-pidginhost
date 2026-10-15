terraform {
  required_providers {
    pidginhost = {
      source = "mihai-amihailesei/pidginhost"
    }
  }
}

# The token is read from the PIDGINHOST_TOKEN environment variable when it is
# not set here. Never commit a token to version control.
provider "pidginhost" {}
