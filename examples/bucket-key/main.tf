# Un bucket de sauvegarde et sa clé S3 scopée — le seul moyen d'y écrire
# depuis une clé d'API de portée `write` : la master key du tenant lui est
# refusée par IAM (voir la note de `ccp_object_bucket`).

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 6.5"
    }
  }
}

resource "ccp_object_bucket" "backup" {
  name   = "ocohi-backup"
  region = "RNN"
}

resource "ccp_bucket_key" "backup_writer" {
  bucket_id       = ccp_object_bucket.backup.id
  label           = "ocohi-backup-writer"
  access_level    = "readwrite"
  expires_in_days = 365
}

# `s3_bucket_name` diffère du nom affiché : c'est celui qu'attendent les outils
# externes (backend Terraform, aws cli, boto3).
output "s3" {
  value = {
    endpoint = ccp_bucket_key.backup_writer.endpoint_url
    bucket   = ccp_bucket_key.backup_writer.s3_bucket_name
    access   = ccp_bucket_key.backup_writer.access_key
  }
  sensitive = true
}
