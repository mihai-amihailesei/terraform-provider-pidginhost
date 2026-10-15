resource "pidginhost_bucket" "backups" {
  name     = "backups"
  quota_gb = 100
}

# The keys end up in the Terraform state: keep the state encrypted and access-controlled.
resource "pidginhost_bucket_credentials" "backups" {
  bucket_id = pidginhost_bucket.backups.id

  # Change this value to rotate the keys.
  rotation_trigger = "2026-10"
}

output "backups_access_key" {
  value     = pidginhost_bucket_credentials.backups.access_key
  sensitive = true
}
