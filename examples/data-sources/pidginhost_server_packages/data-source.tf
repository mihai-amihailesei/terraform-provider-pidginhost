data "pidginhost_server_packages" "all" {}

# The package with the least memory.
locals {
  by_memory        = { for p in data.pidginhost_server_packages.all.packages : format("%012d|%s", p.memory, p.slug) => p.slug }
  smallest_package = local.by_memory[sort(keys(local.by_memory))[0]]
}
