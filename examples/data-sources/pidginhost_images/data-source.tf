data "pidginhost_images" "debian" {
  family_name = "Debian"
}

# The default (usually newest) image of the family.
locals {
  debian_image = one([for i in data.pidginhost_images.debian.images : i.slug if i.is_default])
}
