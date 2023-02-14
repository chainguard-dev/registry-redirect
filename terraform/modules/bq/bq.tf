provider "google" {
  project = var.project
}

resource "google_logging_project_sink" "bq_sink" {
  name        = "bq-sink"
  description = "collecting requests"

  bigquery_options {
    use_partitioned_tables = false
  }
  destination            = "bigquery.googleapis.com/projects/${var.project}/datasets/${google_bigquery_dataset.logs.dataset_id}"
  filter                 = "resource.type = \"cloud_run_revision\""
  unique_writer_identity = true
}

resource "google_bigquery_dataset" "logs" {
  dataset_id                  = "logs"
  default_table_expiration_ms = 90 * 24 * 60 * 60 * 1000 # 90 days
  delete_contents_on_destroy  = false
  location                    = "US"
}
