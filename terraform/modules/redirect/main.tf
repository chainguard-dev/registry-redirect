terraform {
  required_providers {
    ko = {
      source  = "ko-build/ko"
    }
    google = {
      source  = "hashicorp/google"
      version = "~> 4.53.1"
    }
  }
}

provider "ko" {
  repo = "gcr.io/${var.project}"
}

provider "google" {
  project = var.project
}
