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
		{"ghcr.io", "dagger", ""},
	} {
		t.Run(fmt.Sprintf("%s/%s (prefix %s)", c.host, c.repo, c.prefix), func(t *testing.T) {
			s := httptest.NewServer(redirect.New(c.host, c.repo, c.prefix))
			defer s.Close()

			reg := strings.TrimPrefix(s.URL, "http://")
			ref := fmt.Sprintf("%s/engine", reg)
			refWithTag := fmt.Sprintf("%s:main", ref)
			if c.prefix != "" {
				ref = fmt.Sprintf("%s/%s/engine", reg, c.prefix)
			}

			t.Logf("testing image: %s", ref)

			if _, err := crane.Digest(refWithTag); err != nil {
				t.Errorf("digest: %v", err)
			}

			if _, err := crane.Manifest(refWithTag); err != nil {
				t.Errorf("manifest: %v", err)
			}

			if _, err := crane.Pull(refWithTag); err != nil {
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
		image   string
		wantErr bool
	}{
		{"registry.dagger.io with prefix", "registry.dagger.io", "engine", false},
	} {
		t.Run(c.desc, func(t *testing.T) {
			s := httptest.NewServer(redirect.New("ghcr.io", "dagger", ""))
			defer s.Close()

			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v2/%s/tags/list", s.URL, c.image), nil)
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
			all, _ := io.ReadAll(resp.Body)
			if gotErr != c.wantErr {
				t.Errorf("got error %v, want %v; %s", gotErr, c.wantErr, string(all))
			}
			if resp.ContentLength >= 0 && int(resp.ContentLength) != len(all) {
				t.Errorf("got %d bytes, want %d", len(all), resp.ContentLength)
			}

			link := resp.Header.Get("Link")
			if strings.Contains(link, `>; rel="next"`) {
				t.Logf("got Link next header: %s", resp.Header.Get("Link"))
				next := strings.TrimPrefix(link, "<")
				next = next[:strings.Index(next, ">")]

				req, err = http.NewRequest(http.MethodGet, s.URL+next, nil)
				if err != nil {
					t.Fatalf("creating request: %v", err)
				}
				req.Host = c.reqHost
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("listing next page; got status %v, want %v", resp.StatusCode, http.StatusOK)
				}
				all, _ := io.ReadAll(resp.Body)
				if resp.ContentLength >= 0 && int(resp.ContentLength) != len(all) {
					t.Errorf("got %d bytes, want %d", len(all), resp.ContentLength)
				}
			}
		})
	}
}
