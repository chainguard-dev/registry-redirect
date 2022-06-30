terraform {
  required_providers {
    ko = {
      source  = "imjasonh/ko"
      version = "0.0.1"
    }
    google = {
      source  = "hashicorp/google"
      version = "4.26.0"
    }
  }
}

provider "ko" {
  docker_repo = "gcr.io/${var.project}"
}

variable "project" {
  type    = string
}
variable "region" {
  type    = string
  default = "us-east4"
}

provider "google" {
  project = var.project
}

resource "ko_image" "redirect" {
  importpath = "github.com/chainguard-dev/registry-redirect"
}

resource "google_cloud_run_service" "svc" {
  name     = "redirect"
  location = var.region
  template {
    spec {
      containers {
        image = ko_image.redirect.image_ref
      }
      service_account_name = google_service_account.sa.email
    }
  }
  traffic {
    percent         = 100
    latest_revision = true
  }
}

output "url" {
  value = google_cloud_run_service.svc.status[0].url
}

// Anybody can access the service.
data "google_iam_policy" "noauth" {
  binding {
    role    = "roles/run.invoker"
    members = ["allUsers"]
  }
}

resource "google_cloud_run_service_iam_policy" "noauth" {
  location    = google_cloud_run_service.svc.location
  project     = google_cloud_run_service.svc.project
  service     = google_cloud_run_service.svc.name
  policy_data = data.google_iam_policy.noauth.policy_data
}

// The service runs as a minimal service account with no permissions in the project.

resource "google_service_account" "sa" {
  account_id   = "redirect-sa"
  display_name = "Minimal Service Account"
}

// TODO: domain mapping
