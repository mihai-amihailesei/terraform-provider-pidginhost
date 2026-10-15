resource "pidginhost_private_network" "backend" {
  address = "10.10.0.0/24"
  gateway = "10.10.0.1"
}
