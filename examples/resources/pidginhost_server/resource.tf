resource "pidginhost_ssh_key" "admin" {
  alias      = "admin"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "pidginhost_firewall_rules_set" "web" {
  name = "web"
}

resource "pidginhost_server" "web" {
  hostname   = "web-1"
  image      = "ubuntu-24-04" # see the pidginhost_images data source
  package    = "cloud-s"      # see the pidginhost_server_packages data source
  ssh_key_id = pidginhost_ssh_key.admin.id
  new_ipv4   = true
  project    = "website"

  user_data = <<-EOT
    #!/bin/sh
    apt-get update && apt-get install -y nginx
  EOT

  # Applied before first boot; rules live in pidginhost_firewall_rule.
  firewall_rules_set_id = pidginhost_firewall_rules_set.web.id
  firewall_policy_in    = "DROP"

  destroy_protection = true
}

output "web_ipv4" {
  value = pidginhost_server.web.ipv4_address
}
