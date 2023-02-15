terraform {
  backend "gcs" {
    bucket = "artifacts.jason-chainguard.appspot.com"
    prefix = "/registry-redirect-tf-state"
  }
}

module "redirect" {
  source = "../../terraform/modules/redirect"

  project = var.project
  regions = var.regions
}

module "bq" {
  source = "../../terraform/modules/bq"

  project = var.project
}

module "gclb" {
  source = "../../terraform/modules/gclb"

  project       = var.project
  regions       = var.regions
  domains = [
    "redirect.imjasonh.dev",
  ]
  primary_domain = "redirect.imjasonh.dev"
}
