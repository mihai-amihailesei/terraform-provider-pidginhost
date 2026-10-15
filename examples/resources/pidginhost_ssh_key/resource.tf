resource "pidginhost_ssh_key" "deploy" {
  alias      = "deploy"
  public_key = file("~/.ssh/id_ed25519.pub")
}
