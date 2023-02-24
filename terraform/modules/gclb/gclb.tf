provider "google" {
  project = var.project
}

// Reserve a global static IP address.
resource "google_compute_global_address" "global" {
  name = "new-address"
}

resource "google_compute_global_forwarding_rule" "global" {
  name       = "new-global"
  target     = google_compute_target_https_proxy.global.id
  port_range = "443"
  ip_address = google_compute_global_address.global.address
}

resource "google_compute_url_map" "global" {
  name            = "new-global"
  description     = "direct traffic to the backend service"
  default_service = google_compute_backend_service.global.id

  host_rule {
    hosts        = var.domains
    path_matcher = "matcher"
  }

  path_matcher {
    name = "matcher"

    # Match /v2/* and /token and /chainguard/* and send to the backend service.
    path_rule {
      paths   = ["/v2", "/v2/*", "/token", "/chainguard/*"]
      service = google_compute_backend_service.global.id
    }

    # Match all other path and redirect to the Chainguard Images marketing page.
    # See also:
    # https://cloud.google.com/load-balancing/docs/https/setting-up-global-traffic-mgmt#configure_a_url_redirect
    default_url_redirect {
      host_redirect          = "chainguard.dev"
      https_redirect         = false
      path_redirect          = "/chainguard-images"
      redirect_response_code = "TEMPORARY_REDIRECT"
      strip_query            = true
    }
  }

  test {
    service = google_compute_backend_service.global.id
    host    = "cgr.dev"
    path    = "/v2/chainguard/static/manifests/latest"
  }

  test {
    service = google_compute_backend_service.global.id
    host    = "cgr.dev"
    path    = "/chainguard/static:latest"
  }

  test {
    service = google_compute_backend_service.global.id
    host    = "distroless.dev"
    path    = "/v2/static/manifests/latest"
  }
}

resource "google_compute_target_https_proxy" "global" {
  name    = "new-global"
  url_map = google_compute_url_map.global.id

  certificate_map = "//certificatemanager.googleapis.com/${google_certificate_manager_certificate_map.map.id}"
}

// Create a global backend service with a backend for each regional NEG.
resource "google_compute_backend_service" "global" {
  name       = "new-global"
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

// Create a regional network endpoint group (NEG) for each regional Cloud Run service.
resource "google_compute_region_network_endpoint_group" "neg" {
  for_each = var.regions

  name                  = each.key
  network_endpoint_type = "SERVERLESS"
  region                = each.key
  cloud_run {
    service = each.key
  }

  depends_on = [google_project_service.compute]
}

// Enable Compute Engine API.
resource "google_project_service" "compute" {
  disable_on_destroy = false
  service = "compute.googleapis.com"
}
