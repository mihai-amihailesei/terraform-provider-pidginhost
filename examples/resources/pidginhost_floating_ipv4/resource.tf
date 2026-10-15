resource "pidginhost_floating_ipv4" "vip" {
  label       = "web-vip"
  reverse_dns = "www.example.com"
}
