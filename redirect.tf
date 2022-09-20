terraform {
  required_providers {
    ko = {
      source  = "chainguard-dev/ko"
      version = "0.0.2"
    }
    google = {
      source  = "hashicorp/google"
      version = "~> 4.36.0"
    }
  }
}

provider "ko" {
  docker_repo = "gcr.io/${var.project}"
}

variable "project" {
  type = string
}

provider "google" {
  project = var.project
}

// Enable Cloud Run API.
resource "google_project_service" "run" {
  service = "run.googleapis.com"
}

// Enable Compute Engine API.
resource "google_project_service" "compute" {
  service = "compute.googleapis.com"
}

// The service runs as a minimal service account with no permissions in the project.
resource "google_service_account" "sa" {
  account_id   = "redirect-sa"
  display_name = "Minimal Service Account"
}

resource "ko_image" "redirect" {
  importpath = "github.com/chainguard-dev/registry-redirect"
}

resource "google_cloud_run_service" "regions" {
  for_each = var.regions

  name     = each.key
  location = each.key
  template {
    spec {
      containers {
        image = ko_image.redirect.image_ref
        env {
          name  = "REGION"
          value = each.key
        }
        args = [
          "--prefix",
          "chainguard",
          "--repo",
          "chainguard-images",
        ]
      }
      service_account_name  = google_service_account.sa.email
      container_concurrency = 1000
    }
  }
  traffic {
    percent         = 100
    latest_revision = true
  }

  // This is supposed to prevent permanent "Still modifying..." states.
  // See https://github.com/hashicorp/terraform-provider-google/issues/9438
  autogenerate_revision_name = true

  depends_on = [google_project_service.run]
}

// Output each service URL.
output "urls" {
  value = {
    for reg in google_cloud_run_service.regions :
    reg.name => reg.status[0].url
  }
}

// Make each service invokable by all users.
resource "google_cloud_run_service_iam_member" "allUsers" {
  for_each = google_cloud_run_service.regions

  service  = google_cloud_run_service.regions[each.key].name
  location = each.key
  role     = "roles/run.invoker"
  member   = "allUsers"

  depends_on = [google_cloud_run_service.regions]
}
