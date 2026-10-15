resource "pidginhost_private_network" "backend" {
  address = "10.10.0.0/24"
}

resource "pidginhost_private_network_attachment" "db" {
  private_network_id = pidginhost_private_network.backend.id
  server_id          = pidginhost_server.db.id
  address            = "10.10.0.10" # optional; assigned automatically when omitted
}
