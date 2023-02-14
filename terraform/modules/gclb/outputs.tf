output "global_ip" {
  value = google_compute_global_address.global.address
}

output "dns-auth" {
  value = {
    for auth in google_certificate_manager_dns_authorization.this :
    auth.domain => auth.dns_resource_record
  }
}
