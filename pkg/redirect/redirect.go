/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package redirect

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/hashicorp/go-retryablehttp"
	"knative.dev/pkg/logging"
)

var prefixlessHosts = map[string]bool{
	"distroless.dev":   true,
	"images.wolfi.dev": true,
}

func redact(in http.Header) http.Header {
	h := in.Clone()
	if h.Get("Authorization") != "" {
		h.Set("Authorization", "REDACTED")
	}
	return h
}

func New(host, repo, prefix string) http.Handler {
	rdr := redirect{
		host:      host,
		repo:      repo,
		prefix:    prefix,
		retryhttp: retryablehttp.NewClient(),
	}
	router := mux.NewRouter()

	router.Handle("/", http.RedirectHandler("https://github.com/chainguard-images", http.StatusTemporaryRedirect))

	router.HandleFunc("/v2", rdr.v2)
	router.HandleFunc("/v2/", rdr.v2)

	router.HandleFunc("/token", rdr.token)

	router.HandleFunc("/v2/{repo:.*}/manifests/{tagOrDigest:.*}", rdr.proxy)
	router.HandleFunc("/v2/{repo:.*}/blobs/{digest:.*}", rdr.proxy)
	router.HandleFunc("/v2/{repo:.*}/tags/list", rdr.proxy)

	router.NotFoundHandler = http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		logger := logging.FromContext(ctx)
		logger.Infow("got request",
			"method", req.Method,
			"url", req.URL.String(),
			"header", redact(req.Header))
		resp.WriteHeader(http.StatusNotFound)
	})
	return router
}

type redirect struct {
	host      string
	repo      string
	prefix    string
	retryhttp *retryablehttp.Client
}

func (rdr redirect) v2(resp http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	logger := logging.FromContext(ctx)

	var url string
	if rdr.host == "gcr.io" {
		url = "https://gcr.io/v2/"
	} else {
		url = "https://ghcr.io/v2/"
	}
	out, _ := http.NewRequest(req.Method, url, nil)

	logger.Infow("sending request",
		"method", req.Method,
		"url", req.URL.String(),
		"header", redact(req.Header))
	resp.Header().Set("X-Redirected", req.URL.String())

	back, err := rdr.retryhttp.Do(&retryablehttp.Request{Request: out})
	if err != nil {
		logger.Errorf("Error sending request: %v", err)
		http.Error(resp, err.Error(), http.StatusInternalServerError)
		return
	}
	defer back.Body.Close()

	logger.Infow("got response",
		"method", req.Method,
		"url", req.URL.String(),
		"status", back.Status,
		"header", redact(back.Header))

	for k, v := range back.Header {
		for _, vv := range v {
			if k == "Www-Authenticate" {
				log.Println("=== BEFORE: Www-Authenticate:", vv)
				if rdr.host == "gcr.io" {
					// GCR's token endpoint is /v2/token, we want callers to hit us at /token.
					vv = strings.Replace(vv, `realm="https://gcr.io/v2/`, fmt.Sprintf(`realm="https://%s/`, req.Host), 1)
				} else {
					vv = strings.Replace(vv, `realm="https://ghcr.io/`, fmt.Sprintf(`realm="https://%s/`, req.Host), 1)
				}
				log.Println("=== CHANGED: Www-Authenticate:", vv)
			}
			resp.Header().Add(k, vv)
		}
	}
	resp.WriteHeader(back.StatusCode)
	if _, err := io.Copy(resp, back.Body); err != nil {
		logger.Errorf("Error copying response body: %v", err)
	}
}

func (rdr redirect) token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logging.FromContext(ctx)

	vals := r.URL.Query()
	if rdr.prefix != "" {
		scope := vals.Get("scope")
		scope = strings.Replace(scope, rdr.prefix+"/", "", 1)
		vals.Set("scope", scope)
	}
	if rdr.repo != "" {
		scope := vals.Get("scope")
		scope = strings.Replace(scope, "repository:", "repository:"+rdr.repo+"/", 1)
		vals.Set("scope", scope)
	}

	var url string
	if rdr.host == "gcr.io" {
		url = "https://gcr.io/v2/token?" + vals.Encode()
	} else {
		url = "https://ghcr.io/token?" + vals.Encode()
	}

	req, _ := http.NewRequest(r.Method, url, nil)
	req.Header = r.Header.Clone()

	logger.Infow("sending request",
		"method", req.Method,
		"url", req.URL.String(),
		"header", redact(req.Header))
	w.Header().Set("X-Redirected", req.URL.String())

	resp, err := rdr.retryhttp.Do(&retryablehttp.Request{Request: req})
	if err != nil {
		logger.Errorf("Error sending request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	logger.Infow("got response",
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.Status,
		"header", redact(resp.Header))

	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		logger.Errorf("Error copying response body: %v", err)
	}
}

func (rdr redirect) proxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logging.FromContext(ctx)

	var url string
	if rdr.host == "gcr.io" {
		url = "https://gcr.io/v2/"
	} else {
		url = "https://ghcr.io/v2/"
	}
	if rdr.repo != "" {
		url += rdr.repo + "/"
	}

	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	if rdr.prefix != "" && !prefixlessHosts[r.Host] {
		log.Println("=== BEFORE: path:", path)
		// Require and trim the prefix, if the request isn't coming from a prefixless host.
		if !strings.HasPrefix(path, rdr.prefix+"/") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintln(w, `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"Manifest unknown, prefix required"}]}`)
			return
		}
		path = strings.TrimPrefix(path, rdr.prefix+"/")
		log.Println("=== AFTER: path:", path)
	}

	url += path
	if query := r.URL.Query().Encode(); query != "" {
		url += "?" + query
	}
	req, _ := http.NewRequest(r.Method, url, nil)
	req.Header = r.Header.Clone()

	// If the request is coming in without auth, get some auth.
	// This is useful for testing, but should never happen in real life.
	// Actually, containerd seems to make unauthenticated HEAD requests before
	// hitting /v2/, so this might be load-bearing.
	if req.Header.Get("Authorization") == "" {
		logger.Warnw("request without Authorization header, getting auth")
		t, resp, err := rdr.getToken(r)
		if err != nil {
			if resp != nil {
				logger.Infof("Error response getting token: %d %s", resp.StatusCode, resp.Status)
				http.Error(w, resp.Status, resp.StatusCode)
				return
			}
			logger.Errorf("Error getting token: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+t)
	}

	logger.Infow("sending request",
		"method", req.Method,
		"url", req.URL.String(),
		"header", redact(req.Header))
	w.Header().Set("X-Redirected", req.URL.String())

	resp, err := rdr.retryhttp.StandardClient().Transport.RoundTrip(req) // Transport doesn't follow redirects.
	if err != nil {
		logger.Errorf("Error sending request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	logger.Infow("got response",
		"method", r.Method,
		"url", r.URL.String(),
		"status", resp.Status,
		"header", redact(resp.Header))

	for k, v := range resp.Header {
		for _, vv := range v {
			// List responses include a response header to support pagination, that looks like:
			//   Link: </v2/distroless/static/tags/list?n=100&last=blah>; rel="next">
			//
			// In order for the client to be able to use this link, we need to rewrite it to
			// point to the user's requested repo, not the upstream:
			//   Link: </v2[/prefix]/static/repo/tags/list?n=100&last=blah>; rel="next">
			if k == "Link" && strings.HasPrefix(vv, "</v2/"+rdr.repo) {
				log.Println("=== BEFORE: Link:", vv)
				rest := strings.TrimPrefix(vv, "</v2/"+rdr.repo)
				vv = "</v2" + rest
				if rdr.prefix != "" && !prefixlessHosts[r.Host] {
					vv = "</v2/" + rdr.prefix + rest
				}
				log.Println("=== CHANGED: Link:", vv)
			}

			w.Header().Add(k, vv)
		}
	}

	// If it's a list request, rewrite the response so the name key matches the
	// user's requested repo, otherwise clients will repeatedly request the
	// first page looking for their repo's tags.
	if rdr.repo != "" && strings.Contains(r.URL.Path, "/tags/list") {
		var lr listResponse
		if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
			logger.Errorf("Error decoding list response body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Println("=== BEFORE: Name:", lr.Name)
		lr.Name = strings.Replace(lr.Name, rdr.repo+"/", "", 1)
		log.Println("=== CHANGED: Name:", lr.Name)

		// Unset the content-length header from our response, because we're
		// about to rewrite the response to be shorter than the original.
		// This can confuse Cloud Run, which responds with an empty body
		// if the content-length header is wrong in some cases.
		w.Header().Del("Content-Length")
		w.WriteHeader(resp.StatusCode)
		if err := json.NewEncoder(w).Encode(lr); err != nil {
			logger.Errorf("Error encoding list response body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		return
	} else {
		w.WriteHeader(resp.StatusCode)
	}

	// Unless we're serving blobs, also proxy the response body, if any.
	// Most of the time blob responses will just be 302 redirects to
	// another location, likely a CDN, but just in case we get a "real"
	// response we'd like to avoid paying the egress cost to serve it.
	// Manifests may also be served with redirects, but if they're not,
	// they're likely small enough we don't mind paying to proxy them.
	parts := strings.Split(r.URL.Path, "/")
	if parts[len(parts)-2] != "blobs" {
		if _, err := io.Copy(w, resp.Body); err != nil {
			logger.Errorf("Error copying response body: %v", err)
		}
	}
}

func (rdr redirect) getToken(r *http.Request) (string, *http.Response, error) {
	parts := strings.Split(r.URL.Path, "/")
	parts = parts[2 : len(parts)-2]
	if rdr.prefix != "" && parts[0] == rdr.prefix {
		parts = parts[1:]
	}
	if rdr.repo != "" {
		parts = append([]string{rdr.repo}, parts...)
	}
	var url string
	if rdr.host == "gcr.io" {
		url = fmt.Sprintf("https://gcr.io/v2/token?scope=repository:%s:pull&service=gcr.io", strings.Join(parts, "/"))
	} else {
		url = fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull&service=ghcr.io", strings.Join(parts, "/"))
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header = r.Header.Clone()
	resp, err := rdr.retryhttp.Do(&retryablehttp.Request{Request: req}) //nolint:gosec
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", resp, fmt.Errorf("Error getting token: %v", resp.Status)
	}
	var t struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return "", nil, err
	}
	return t.Token, nil, nil
}

type listResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}
