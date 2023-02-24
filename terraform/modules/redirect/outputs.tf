
// Output each service URL.
output "urls" {
  value = {
    for reg in google_cloud_run_service.regions :
    reg.name => reg.status[0].url
  }
}
