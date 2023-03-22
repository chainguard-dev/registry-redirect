output "global_ip" {
  value = google_compute_global_address.global.address
}

output "dns-auth" {
  value = google_certificate_manager_dns_authorization.this.dns_resource_record
}
