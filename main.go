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

func main() {
	flag.Parse()
	logger := logging.FromContext(context.Background())

	http.Handle("/", redirect.New())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Infof("Listening on port %s", port)
	logger.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}
