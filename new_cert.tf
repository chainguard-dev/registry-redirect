variable "new_domains" {
  type = list(string)
  default = [
    "cgr.dev",
    "distroless.dev",
    "images.wolfi.dev",
  ]
}

// Enable Certificate Manager API.
resource "google_project_service" "certmanager" {
  service = "certificatemanager.googleapis.com"
}

resource "google_certificate_manager_dns_authorization" "this" {
  for_each = toset(var.new_domains)
  name     = replace("${each.key}", ".", "-")
  domain   = each.key
  labels   = {}
}

resource "google_certificate_manager_certificate" "cert" {
  for_each = toset(var.new_domains)

  name  = replace("${each.key}", ".", "-")
  scope = "DEFAULT"

  managed {
    domains = [each.key]
    dns_authorizations = [
      google_certificate_manager_dns_authorization.this[each.key].id
    ]
  }

  depends_on = [google_project_service.certmanager]
}

resource "google_certificate_manager_certificate_map" "map" {
  name = "cert-map"
}

resource "google_certificate_manager_certificate_map_entry" "map_entry" {
  for_each = toset(var.new_domains)

  name     = replace("certificatemapentry-${each.key}", ".", "-")
  map      = google_certificate_manager_certificate_map.map.name
  hostname = each.key

  certificates = [
    google_certificate_manager_certificate.cert[each.key].id
  ]
}

