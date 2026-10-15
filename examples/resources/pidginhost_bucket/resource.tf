resource "pidginhost_bucket" "assets" {
  name        = "assets"
  quota_gb    = 50
  public_read = true
}

output "assets_url" {
  value = "${pidginhost_bucket.assets.endpoint}/${pidginhost_bucket.assets.full_name}"
}
