// Enable Cloud Run API.
resource "google_project_service" "run" {
  disable_on_destroy = false
  service            = "run.googleapis.com"
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


// Make each service invokable by all users.
resource "google_cloud_run_service_iam_member" "allUsers" {
  for_each = var.regions

  service  = google_cloud_run_service.regions[each.key].name
  location = each.key
  role     = "roles/run.invoker"
  member   = "allUsers"

  depends_on = [google_cloud_run_service.regions]
}
