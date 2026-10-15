variable "name" {
  description = "Prefix for every resource name."
  type        = string
  default     = "demo"
}

variable "ssh_public_key" {
  description = "SSH public key installed on both servers."
  type        = string
}

variable "image_family" {
  description = "OS image family; the family's default image is used."
  type        = string
  default     = "Debian"
}

variable "admin_cidr" {
  description = "Network allowed to reach the servers over SSH."
  type        = string
  default     = "198.51.100.0/24"
}

variable "private_cidr" {
  description = "Address range of the private network."
  type        = string
  default     = "10.50.0.0/24"
}
