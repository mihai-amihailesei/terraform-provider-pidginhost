resource "pidginhost_floating_ipv6" "vip" {
  label       = "web-vip"
  reverse_dns = "www.example.com"
}
