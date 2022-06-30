# OCI Registry Redirector

This is a simple OCI redirector service that allows for custom domains, including forwarding auth token requests to the original registry.

For example, this is used to serve `distroless.dev/*` as a redirection to `ghcr.io/distroless/*`.

It's intended to be deployed to Google Cloud Run, which is responsible for handling HTTPS.

## Deploying

First make sure you're logged in to GCP and initialize the Terraform depedencies:

```
gcloud auth login
gcloud auth application-default login
terraform init
```

Then bild and deploy the service with:

```
$ terraform apply -var project=[MY-PROJECT]
...
url = "https://redirect-a1b2c3d4-uk.a.run.app"
```

This requires permission to push images to GCR, and to deploy Cloud Run services.

This will deploy to `us-east4` -- you can override with `-var region=[MY-REGION]`.

## Auth

To authorize through the redirection step (e.g., to access private images), you can configure credentials for the domain where the redirector is hosted.

This may be a Cloud Run service URL (something like `redirect-a1b2c3d4-uk.a.run.app`) or a mapped domain name.

When used this way, registry credentials are sent to the redirector, and are passed directly on to the real registry.
Credentials are never stored or logged by the redirector.

### GCR Auth

To configure auth to GCR, you can either:

- download a Service Account's JSON key as described in [GCR docs](https://cloud.google.com/container-registry/docs/advanced-authentication#json-key), or
- configure the [`docker-credential-gcr` cred helper](https://cloud.google.com/container-registry/docs/advanced-authentication#standalone-helper)

In either case, make sure to configure those creds _for the redirector's domain_, not `*.gcr.io`, e.g.:

```
cat keyfile.json | docker login redirect-a1b2c3d4-uk.a.run.app -u _json_key --password-stdin
```

or, in your `~/.docker/config.json`:

```
{
  "credHelpers": {
    "redirect-a1b2c3d4-uk.a.run.app": "gcr",
  }
}
```

This will tell clients to use GCR creds to talk to the redirector, which will be passed through the redirector to GCR when you make requests.

### GHCR Auth

To configure auth to GHCR, you can use a personal access token as described in [GHCR docs](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).

Make sure to configure those creds _for the redirector's domain_, not `ghcr.io`, e.g.:

```
echo $CR_PAT | docker login redirect-a1b2c3d4-uk.a.run.app -u USERNAME --password-stdin
```

This will tell clients to use GHCR creds to talk to the redirector, which will be passed through the redirector to GHCR when you make requests.

## Configuration

You can use this to host other redirections, to ghcr.io (the default) or gcr.io (using `--gcr=true`).
