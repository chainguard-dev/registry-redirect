variable "project" {
  type = string
}

variable "regions" {
  type = set(string)
  default = [
    "us-east4",        // Virginia
    "europe-west1",    // Belgium
    "asia-northeast1", // Japan
  ]
}

variable "domain" {
  type = string
}
