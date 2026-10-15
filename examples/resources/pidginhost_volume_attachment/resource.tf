# Attach a volume that Terraform did not create (for example one made in the
# control panel) to a server.
resource "pidginhost_volume_attachment" "archive" {
  volume_id = "123"
  server_id = pidginhost_server.db.id
}
