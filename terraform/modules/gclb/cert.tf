

// Enable Certificate Manager API.
resource "google_project_service" "certmanager" {
  disable_on_destroy = false
  service = "certificatemanager.googleapis.com"
}

resource "google_certificate_manager_dns_authorization" "this" {
  name     = replace("${var.domain}", ".", "-")
  domain   = var.domain
  labels   = {}
}

resource "google_certificate_manager_certificate" "cert" {
  name  = replace("${var.domain}", ".", "-")
  scope = "DEFAULT"

  managed {
    domains = [var.domain]
  }

  depends_on = [google_project_service.certmanager]
}

resource "google_certificate_manager_certificate_map" "map" {
  name = "cert-map"
}

resource "google_certificate_manager_certificate_map_entry" "map_entry" {
  name     = replace("certificatemapentry-${var.domain}", ".", "-")
  map      = google_certificate_manager_certificate_map.map.name
  matcher = "PRIMARY"

  certificates = [
    google_certificate_manager_certificate.cert.id
  ]
}
