/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/chainguard-dev/registry-redirect/pkg/redirect"
	"knative.dev/pkg/logging"
)

// TODO:
// - Also support anonymous and Basic-type auth?
// - take a config for registries/repos to redirect from/to.

var (
	// Redirect requests for $DOMAIN/[prefix/]static -> ghcr.io/chainguard-images/static
	// If repo is empty, example.dev/foo/bar -> ghcr.io/foo/bar
	repo = flag.String("repo", "chainguard-images", "repo to redirect to")

	// TODO(jason): Support arbitrary registries.
	gcr = flag.Bool("gcr", false, "if true, use GCR mode")

	// prefix is the user-visible repo prefix.
	// For example, if repo is "chainguard-images" and prefix is "unicorns",
	// users hitting example.dev/unicorns/foo/bar will be redirected to
	// ghcr.io/chainguard-images/foo/bar.
	// If prefix is unset, hitting example.dev/unicorns/foo/bar will
	// redirect to ghcr.io/unicorns/foo/bar.
	// If prefix is set, and users hit a path without the prefix, it's ignored:
	// - example.dev/foo/bar -> ghcr.io/chainguard-images/foo/bar
	// (this is for backward compatibility with prefix-less redirects)
	prefix = flag.String("prefix", "chainguard", "if set, user-visible repo prefix")
)

func main() {
	flag.Parse()
	logger := logging.FromContext(context.Background())

	host := "ghcr.io"
	if *gcr {
		host = "gcr.io"
	}
	r := redirect.New(host, *repo, *prefix)
	http.Handle("/", r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Infof("Listening on port %s", port)
	logger.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}
