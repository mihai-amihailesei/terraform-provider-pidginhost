resource "pidginhost_volume" "data" {
  server_id = pidginhost_server.db.id
  product   = "ssd" # see the pidginhost_storage_products data source
  size      = 100   # GB; can grow in place
  alias     = "db-data"
}
