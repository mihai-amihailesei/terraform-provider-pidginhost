output "server_ipv4" {
  description = "Public IPv4 address of each server."
  value       = { for k, s in pidginhost_server.web : k => s.ipv4_address }
}

output "server_private_ip" {
  description = "Private address of each server."
  value       = { for k, a in pidginhost_private_network_attachment.web : k => a.address }
}

output "floating_ip" {
  description = "Floating IPv4 shared by the servers."
  value       = pidginhost_floating_ipv4.vip.address
}

output "bucket" {
  description = "Bucket name and endpoint for S3 clients."
  value = {
    name     = pidginhost_bucket.assets.full_name
    endpoint = pidginhost_bucket.assets.endpoint
    region   = pidginhost_bucket.assets.region
  }
}

output "bucket_access_key" {
  description = "S3 access key of the bucket."
  value       = pidginhost_bucket_credentials.assets.access_key
  sensitive   = true
}

output "bucket_secret_key" {
  description = "S3 secret key of the bucket."
  value       = pidginhost_bucket_credentials.assets.secret_key
  sensitive   = true
}
