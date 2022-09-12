#!/usr/bin/env bash

set -euxo pipefail

crane version >/dev/null \
  || { echo "install crane: https://github.com/google/go-containerregistry/blob/main/cmd/crane"; exit 1; }

# Run the redirector in the background, kill it when the script exits.
go build && ./registry-redirect --prefix=unicorns &
PID=$!
echo "server running with pid $PID"
trap 'kill $PID' EXIT

sleep 3  # Server isn't immediately ready.

crane digest localhost:8080/nginx
crane manifest localhost:8080/nginx
crane ls localhost:8080/nginx

crane digest localhost:8080/unicorns/nginx
crane manifest localhost:8080/unicorns/nginx
crane ls localhost:8080/unicorns/nginx

# TODO(jason): docker pull an image through the redirector.

# TODO(jason): Run the redirector as a container connected to a kind cluster
# and pull through the redirector
