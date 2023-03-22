#!/usr/bin/env bash

set -euxo pipefail

crane version >/dev/null \
  || { echo "install crane: https://github.com/google/go-containerregistry/blob/main/cmd/crane"; exit 1; }

# Kill whatever's running on :8080
# kill -9 $(lsof -ti:8080)

# Run the redirector in the background, kill it when the script exits.
go build && ./registry-redirect &
PID=$!
echo "server running with pid $PID"
trap 'kill $PID' EXIT

sleep 3  # Server isn't immediately ready.

crane digest localhost:8080/nginx
crane manifest localhost:8080/nginx
crane ls localhost:8080/nginx
crane pull localhost:8080/nginx /dev/null

echo PASSED
