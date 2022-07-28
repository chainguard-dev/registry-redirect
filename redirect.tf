terraform {
  required_providers {
    ko = {
      source  = "chainguard-dev/ko"
      version = "0.0.2"
    }
    google = {
      source  = "hashicorp/google"
      version = "4.26.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "4.26.0"
    }
  }
}

provider "ko" {
  docker_repo = "gcr.io/${var.project}"
}

variable "project" {
  type = string
}
variable "region" {
  type    = string
  default = "us-east4"
}

provider "google" {
  project = var.project
}

provider "google-beta" {
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

/////
// Legacy single-region app
/////

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
  depends_on = [google_project_service.run]
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

/////
// New hotness multi-region app.
/////

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
      }
      service_account_name = google_service_account.sa.email
    }
  }
  traffic {
    percent         = 100
    latest_revision = true
  }

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
