# OCI Registry Redirector

[![ci](https://github.com/chainguard-dev/registry-redirect/actions/workflows/ci.yaml/badge.svg)](https://github.com/chainguard-dev/registry-redirect/actions/workflows/ci.yaml)

This is a simple OCI redirector service that allows for custom domains, including forwarding auth token requests to the original registry.

For example, this is used to serve `distroless.dev/*` as a redirection to `cgr.dev/chainguard/*`.

It's intended to be deployed to Google Cloud Run behind a GCLB load balancer, which is responsible for handling HTTPS.

## Deploying

First make sure you're logged in to GCP and initialize the Terraform depedencies:

```
gcloud auth login
gcloud auth application-default login
terraform init
```

Then build and deploy the service with:

```
$ terraform apply -var project=[MY-PROJECT]
...
url = "https://redirect-a1b2c3d4-uk.a.run.app"
```

This requires permission to push images to GCR, and to deploy Cloud Run services and create GCP resources.
