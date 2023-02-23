

// Enable Certificate Manager API.
resource "google_project_service" "certmanager" {
  disable_on_destroy = false
  service = "certificatemanager.googleapis.com"
}

resource "google_certificate_manager_dns_authorization" "this" {
  for_each = var.domains
  name     = replace("${each.key}", ".", "-")
  domain   = each.key
  labels   = {}
}

resource "google_certificate_manager_certificate" "cert" {
  for_each = var.domains

  name  = replace("${each.key}", ".", "-")
  scope = "DEFAULT"

  managed {
    domains = [each.key]
  }

  depends_on = [google_project_service.certmanager]
}

resource "google_certificate_manager_certificate_map" "map" {
  name = "cert-map"
}

resource "google_certificate_manager_certificate_map_entry" "map_entry" {
  for_each = var.domains

  name     = replace("certificatemapentry-${each.key}", ".", "-")
  map      = google_certificate_manager_certificate_map.map.name

  hostname = each.key

  certificates = [
    google_certificate_manager_certificate.cert[each.key].id
  ]
}

resource "google_certificate_manager_certificate_map_entry" "primary_map_entry" {
  name     = replace("certificatemapentry-${var.primary_domain}-2", ".", "-")
  map      = google_certificate_manager_certificate_map.map.name
  matcher = "PRIMARY"

  certificates = [
    google_certificate_manager_certificate.cert[var.primary_domain].id
  ]
}

