/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package redirect_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainguard-dev/registry-redirect/pkg/redirect"
	"github.com/google/go-containerregistry/pkg/crane"
)

func TestRedirect(t *testing.T) {
	s := httptest.NewServer(redirect.New())
	defer s.Close()

	reg := strings.TrimPrefix(s.URL, "http://")
	ref := fmt.Sprintf("%s/static", reg)

	t.Logf("testing image: %s", ref)

	if _, err := crane.Digest(ref); err != nil {
		t.Errorf("digest: %v", err)
	}

	if _, err := crane.Manifest(ref); err != nil {
		t.Errorf("manifest: %v", err)
	}

	if _, err := crane.Pull(ref); err != nil {
		t.Errorf("pulling: %v", err)
	}

	if _, err := crane.ListTags(ref); err != nil {
		t.Errorf("listing tags: %v", err)
	}
}

func TestGHPageRedirect(t *testing.T) {
	s := httptest.NewServer(redirect.New())

	for _, path := range []string{
		"/",
		"/busybox",
		"/busybox:latest",
		"/busybox@sha256:abcdef",
	} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, s.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			got, err := resp.Location()
			if err != nil {
				t.Fatal(err)
			}
			if got, want := got.String(), "https://cgr.dev/chainguard"+path; got != want {
				t.Fatalf("Got %q, want %q", got, want)
			}
		})
	}
}
