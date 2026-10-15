data "pidginhost_server" "db" {
  hostname = "db-1"
}

output "db_private_addresses" {
  value = data.pidginhost_server.db.private_networks[*].address
}
