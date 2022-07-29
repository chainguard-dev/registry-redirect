variable "regions" {
  type = set(string)
  default = [
    "us-east4",        // Virginia
    "europe-west1",    // Belgium
    "asia-northeast1", // Japan
  ]
}

variable "domains" {
  type = list(any)
  default = [
    "distroless.dev",
    "new.distroless.dev",
  ]
}

// Generate a random certificate name that changes whenever var.domains changes.
resource "random_id" "certificate" {
  byte_length = 2
  prefix      = "global-"
  keepers = {
    domains = join(",", var.domains)
  }
}

resource "google_compute_managed_ssl_certificate" "global" {
  provider = google-beta

  name = random_id.certificate.hex
  managed {
    domains = var.domains
  }
  // If the cert changed, it's because the domains that feed into the random
  // cert name were changed. Create the new cert before destroying the old one.
  lifecycle {
    create_before_destroy = true
  }
}

// Reserve a global static IP address.
resource "google_compute_global_address" "global" {
  name = "address"
}

output "global_ip" {
  value = google_compute_global_address.global.address
}

resource "google_compute_global_forwarding_rule" "global" {
  name       = "global"
  target     = google_compute_target_https_proxy.global.id
  port_range = "443"
  ip_address = google_compute_global_address.global.address
}

resource "google_compute_url_map" "global" {
  provider = google-beta

  name            = "global"
  description     = "direct traffic to the backend service"
  default_service = google_compute_backend_service.global.id
}

resource "google_compute_target_https_proxy" "global" {
  provider = google-beta

  name             = "global"
  url_map          = google_compute_url_map.global.id
  ssl_certificates = [google_compute_managed_ssl_certificate.global.id]
}

// Create a regional network endpoint group (NEG) for each regional Cloud Run service.
resource "google_compute_region_network_endpoint_group" "neg" {
  for_each = google_cloud_run_service.regions

  name                  = each.key
  network_endpoint_type = "SERVERLESS"
  region                = each.key
  cloud_run {
    service = google_cloud_run_service.regions[each.key].name
  }

  depends_on = [google_project_service.compute]
}

// Create a global backend service with a backend for each regional NEG.
resource "google_compute_backend_service" "global" {
  name       = "global"
  enable_cdn = true

  # Inject some request headers based on detected client information.
  # See https://cloud.google.com/load-balancing/docs/https/custom-headers#variables
  custom_request_headers = [
    "x-client-rtt: {client_rtt_msec}",
    "x-client-region: {client_region}",
    "x-client-region-subdivision: {client_region_subdivision}",
    "x-client-city: {client_city}",
  ]

  # Log a sample of requests which we can query later.
  log_config {
    enable      = true
    sample_rate = 0.1
  }

  // Add a backend for each regional NEG.
  dynamic "backend" {
    for_each = google_compute_region_network_endpoint_group.neg
    content {
      group = backend.value["id"]
    }
  }
}

// Create an HTTP->HTTPS upgrade rule.
resource "google_compute_url_map" "https_redirect" {
  name = "https-redirect"

  default_url_redirect {
    https_redirect         = true
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    strip_query            = false
  }
}

resource "google_compute_target_http_proxy" "https_redirect" {
  name    = "https-redirect"
  url_map = google_compute_url_map.https_redirect.id
}

resource "google_compute_global_forwarding_rule" "https_redirect" {
  name = "https-redirect"

  target     = google_compute_target_http_proxy.https_redirect.id
  port_range = "80"
  ip_address = google_compute_global_address.global.address
}
