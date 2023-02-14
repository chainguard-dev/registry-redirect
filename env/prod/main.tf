terraform {
  backend "gcs" {
    bucket = "artifacts.chainguard-images.appspot.com"
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

  project = var.project
  regions = var.regions
  domains = [
    "cgr.dev",
    "distroless.dev",
  ]
}
