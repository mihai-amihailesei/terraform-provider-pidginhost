# Complete example

Two servers on a private network behind a firewall, a floating IPv4 both
servers may use, a data volume, and an object storage bucket with its own
credentials.

```sh
export PIDGINHOST_TOKEN=...            # never commit the token
cp terraform.tfvars.example terraform.tfvars
$EDITOR terraform.tfvars
terraform init                          # or: tofu init
terraform apply
```

This creates billable resources: two of the smallest servers, two public
IPv4 addresses, a floating IPv4, a 10 GB volume and a 1 GB bucket. Run
`terraform destroy` when you are done.

The bucket credentials are stored in the state file. Use an encrypted,
access-controlled state backend (or OpenTofu state encryption) for anything
beyond experiments.
