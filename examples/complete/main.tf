# Two web servers on a private network, behind a firewall, sharing a
# floating IP, with a data volume on the first server and an object storage
# bucket with its own credentials.

data "pidginhost_images" "family" {
  family_name = var.image_family
}

data "pidginhost_server_packages" "all" {}

data "pidginhost_storage_products" "all" {}

locals {
  image = one([for i in data.pidginhost_images.family.images : i.slug if i.is_default])

  # The package with the least memory.
  packages_by_memory = { for p in data.pidginhost_server_packages.all.packages : format("%012d|%s", p.memory, p.slug) => p.slug }
  package            = local.packages_by_memory[sort(keys(local.packages_by_memory))[0]]

  storage_product = data.pidginhost_storage_products.all.products[0].slug

  servers = toset(["web-1", "web-2"])
}

resource "pidginhost_ssh_key" "admin" {
  alias      = "${var.name}-admin"
  public_key = var.ssh_public_key
}

# --- Firewall -------------------------------------------------------------

resource "pidginhost_firewall_rules_set" "web" {
  name = "${var.name}-web"
}

resource "pidginhost_firewall_rule" "ssh" {
  rules_set_id     = pidginhost_firewall_rules_set.web.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  source           = var.admin_cidr
  destination_port = "22"
}

resource "pidginhost_firewall_rule" "http" {
  rules_set_id     = pidginhost_firewall_rules_set.web.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  destination_port = "80,443"
}

resource "pidginhost_firewall_rule" "private" {
  rules_set_id = pidginhost_firewall_rules_set.web.id
  direction    = "in"
  action       = "ACCEPT"
  source       = var.private_cidr
}

# --- Servers --------------------------------------------------------------

resource "pidginhost_server" "web" {
  for_each = local.servers

  hostname   = "${var.name}-${each.key}"
  image      = local.image
  package    = local.package
  ssh_key_id = pidginhost_ssh_key.admin.id
  new_ipv4   = true
  project    = var.name

  # Applied before first boot. Everything not allowed above is dropped.
  firewall_rules_set_id = pidginhost_firewall_rules_set.web.id
  firewall_policy_in    = "DROP"

  user_data = <<-EOT
    #!/bin/sh
    hostnamectl set-hostname ${var.name}-${each.key}
  EOT
}

# --- Private network ------------------------------------------------------

resource "pidginhost_private_network" "backend" {
  address = var.private_cidr
}

resource "pidginhost_private_network_attachment" "web" {
  for_each = pidginhost_server.web

  private_network_id = pidginhost_private_network.backend.id
  server_id          = each.value.id
}

# --- Floating IP shared by both servers -------------------------------------

resource "pidginhost_floating_ipv4" "vip" {
  label = "${var.name}-vip"
}

resource "pidginhost_floating_ip_authorization" "web" {
  for_each = pidginhost_server.web

  floating_ipv4_id = pidginhost_floating_ipv4.vip.id
  server_id        = each.value.id
}

# --- Data volume on the first server ----------------------------------------

resource "pidginhost_volume" "data" {
  server_id = pidginhost_server.web["web-1"].id
  product   = local.storage_product
  size      = 10
  alias     = "${var.name}-data"
}

# --- Object storage ---------------------------------------------------------

resource "pidginhost_bucket" "assets" {
  name     = "${var.name}-assets"
  quota_gb = 1
}

# The keys are stored in the state: keep the state encrypted.
resource "pidginhost_bucket_credentials" "assets" {
  bucket_id        = pidginhost_bucket.assets.id
  rotation_trigger = "initial"
}
