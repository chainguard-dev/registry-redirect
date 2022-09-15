/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package redirect_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainguard-dev/registry-redirect/pkg/redirect"
	"github.com/google/go-containerregistry/pkg/crane"
)

func TestRedirect(t *testing.T) {
	for _, c := range []struct{ host, repo, prefix string }{
		{"ghcr.io", "distroless", ""},
		{"gcr.io", "jason-chainguard-public", ""},
		{"ghcr.io", "distroless", "unicorns"},
		{"gcr.io", "jason-chainguard-public", "unicorns"},
	} {
		t.Run(fmt.Sprintf("%s/%s (prefix %s)", c.host, c.repo, c.prefix), func(t *testing.T) {
			s := httptest.NewServer(redirect.New(c.host, c.repo, c.prefix))
			defer s.Close()

			reg := strings.TrimPrefix(s.URL, "http://")
			ref := fmt.Sprintf("%s/static", reg)
			if c.prefix != "" {
				ref = fmt.Sprintf("%s/%s/static", reg, c.prefix)
			}

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

			// TODO(jason): Listing tags against GCR doesn't work due to gzip;
			// fix this and re-enable, or remove GCR support.
			if c.host != "gcr.io" {
				if _, err := crane.ListTags(ref); err != nil {
					t.Errorf("listing tags: %v", err)
				}
			}
		})
	}
}

func TestPrefixlessHosts(t *testing.T) {
	for _, c := range []struct {
		desc    string
		reqHost string
		repo    string
		wantErr bool
	}{
		{"cgr with prefix", "cgr.dev", "chainguard/static", false},
		{"cgr without prefix", "cgr.dev", "static", true},
		{"distroless with prefix", "distroless.dev", "chainguard/static", true},
		{"distroless without prefix", "distroless.dev", "static", false},
	} {
		t.Run(c.desc, func(t *testing.T) {
			s := httptest.NewServer(redirect.New("ghcr.io", "distroless", "chainguard"))
			defer s.Close()

			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v2/%s/tags/list", s.URL, c.repo), nil)
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}
			req.Host = c.reqHost
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			gotErr := (resp.StatusCode != http.StatusOK)
			if gotErr != c.wantErr {
				all, _ := io.ReadAll(resp.Body)
				t.Errorf("got error %v, want %v; %s", gotErr, c.wantErr, string(all))
			}

			t.Logf("got Link next header: %s", resp.Header.Get("Link"))
		})
	}
}
