resource "pidginhost_firewall_rules_set" "web" {
  name = "web"
}

# Rules are separate resources; see pidginhost_firewall_rule.
# Apply the set to a server with its firewall_rules_set_id attribute.
