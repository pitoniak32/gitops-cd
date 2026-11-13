# Example terraform app module (ADR 0021), pushed as an OCI artifact by `gcd tf-module-push`.
# No provider: a real module would declare google_storage_bucket; terraform_data keeps the demo offline.
terraform {
  required_version = ">= 1.6"
  backend "gcs" {} # bucket/prefix from terraform.yaml via -backend-config
}

resource "terraform_data" "bucket" {
  input = {
    name   = var.bucket_name
    region = var.region
  }
}

output "bucket" {
  value = terraform_data.bucket.output
}
