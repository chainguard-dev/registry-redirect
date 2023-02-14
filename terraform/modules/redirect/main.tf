terraform {
  required_providers {
    ko = {
      source  = "ko-build/ko"
    }
    google = {
      source  = "hashicorp/google"
      version = "~> 4.36.0"
    }
  }
}

provider "ko" {
  repo = "gcr.io/${var.project}"
}

provider "google" {
  project = var.project
}
