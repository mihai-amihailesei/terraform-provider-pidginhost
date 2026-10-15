resource "pidginhost_firewall_rules_set" "web" {
  name = "web"
}

resource "pidginhost_firewall_rule" "ssh_from_office" {
  rules_set_id     = pidginhost_firewall_rules_set.web.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  source           = "198.51.100.0/24"
  destination_port = "22"
}

resource "pidginhost_firewall_rule" "https" {
  rules_set_id     = pidginhost_firewall_rules_set.web.id
  direction        = "in"
  action           = "ACCEPT"
  protocol         = "tcp"
  destination_port = "80,443"
}
