resource "pidginhost_ipv4" "extra" {}

resource "pidginhost_ipv4_attachment" "web_extra" {
  server_id  = pidginhost_server.web.id
  address_id = pidginhost_ipv4.extra.id
  reboot     = true # restart so the guest picks the address up
}
