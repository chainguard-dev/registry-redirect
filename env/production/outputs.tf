output "ip" {
  value = module.gclb.global_ip
}

output "dns-auth" {
  value = module.gclb.dns-auth
}

output "services" {
  value = module.redirect.urls
}
