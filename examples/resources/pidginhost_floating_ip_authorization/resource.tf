resource "pidginhost_floating_ipv4" "vip" {
  label = "web-vip"
}

# Allow both web servers to take over the floating address.
resource "pidginhost_floating_ip_authorization" "web" {
  for_each = {
    web1 = pidginhost_server.web1.id
    web2 = pidginhost_server.web2.id
  }

  floating_ipv4_id = pidginhost_floating_ipv4.vip.id
  server_id        = each.value
}
