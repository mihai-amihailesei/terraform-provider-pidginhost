resource "pidginhost_ipv6" "extra" {}

resource "pidginhost_ipv6_attachment" "web_extra" {
  server_id  = pidginhost_server.web.id
  address_id = pidginhost_ipv6.extra.id
  reboot     = true # restart so the guest picks the address up
}
