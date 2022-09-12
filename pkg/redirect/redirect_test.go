/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package redirect_test

import (
	"fmt"
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
