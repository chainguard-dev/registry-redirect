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
	"os/signal"
	"time"

	"github.com/chainguard-dev/registry-redirect/pkg/redirect"
	"go.uber.org/zap"
	"knative.dev/pkg/logging"
)

// TODO:
// - Also support anonymous and Basic-type auth?
// - take a config for registries/repos to redirect from/to.

var (
	// Redirect requests for example.dev/static -> ghcr.io/static
	// If repo is empty, example.dev/foo/bar -> ghcr.io/foo/bar
	repo = flag.String("repo", "", "repo to redirect to")

	// TODO(jason): Support arbitrary registries.
	gcr = flag.Bool("gcr", false, "if true, use GCR mode")

	// prefix is the user-visible repo prefix.
	// For example, if repo is "example" and prefix is "unicorns",
	// users hitting example.dev/unicorns/foo/bar will be redirected to
	// ghcr.io/example/foo/bar.
	// If prefix is unset, hitting example.dev/unicorns/foo/bar will
	// redirect to ghcr.io/unicorns/foo/bar.
	// If prefix is set, and users hit a path without the prefix, it's ignored:
	// - example.dev/foo/bar -> ghcr.io/distroless/foo/bar
	// (this is for backward compatibility with prefix-less redirects)
	prefix = flag.String("prefix", "", "if set, user-visible repo prefix")
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())

	logger := logging.FromContext(ctx)

	go func() {
		oscall := <-c
		logger.Infof("system call:%+v", oscall)
		cancel()
	}()

	if err := serve(ctx, logger); err != nil {
		logger.Fatalf("failed to serve:+%v\n", err)
	}
}

func serve(ctx context.Context, logger *zap.SugaredLogger) (err error) {
	flag.Parse()
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
	logger.Info("http server starting...")
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: nil,
	}
	go func() {
		if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("listen:%+s\n", err)
		}
	}()
	logger.Infof("http server listening on port: %s", port)
	<-ctx.Done()
	logger.Info("http server stopped")

	ctxShutDown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		cancel()
	}()

	if err = srv.Shutdown(ctxShutDown); err != nil {
		logger.Fatalf("http server shutdown failed:%+s", err)
	}

	logger.Infof("http server shutdown gracefully")

	if err == http.ErrServerClosed {
		err = nil
	}

	return
}
