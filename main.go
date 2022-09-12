/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"go.uber.org/zap"
)

// TODO:
// - Also support anonymous and Basic-type auth?
// - take a config for registries/repos to redirect from/to.

var (
	// Redirect requests for distroless.dev/static -> ghcr.io/distroless/static
	// If repo is empty, example.dev/foo/bar -> ghcr.io/foo/bar
	repo = flag.String("repo", "distroless", "repo to redirect to")

	// TODO(jason): Support arbitrary registries.
	gcr = flag.Bool("gcr", false, "if true, use GCR mode")

	// prefix is the user-visible repo prefix.
	// For example, if repo is "distroless" and prefix is "unicorns",
	// users hitting example.dev/unicorns/foo/bar will be redirected to
	// ghcr.io/distroless/foo/bar.
	// If prefix is unset, hitting example.dev/unicorns/foo/bar will
	// redirect to ghcr.io/unicorns/foo/bar.
	// If prefix is set, users must hit example.dev/unicorns/*; any other request
	// will 404.
	prefix = flag.String("prefix", "", "if set, user-visible repo prefix")
)

var logger *zap.SugaredLogger

func init() {
	l, err := zap.NewProduction(zap.AddCaller())
	if err != nil {
		log.Fatalf("setting up zap logger: %v", err)
	}
	logger = l.Sugar()
}

func main() {
	flag.Parse()

	http.HandleFunc("/v2/", handler)
	http.HandleFunc("/token", handler)

	// TODO(jason): Configure this more generally.
	if *repo == "distroless" {
		http.Handle("/new", http.RedirectHandler("https://github.com/"+*repo+"/template/generate", http.StatusSeeOther))
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			url := "https://github.com/distroless" + r.URL.Path
			http.Redirect(w, r, url, http.StatusSeeOther)
		})
	}

	logger.Info("Starting...")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Infof("Listening on port %s", port)
	logger.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

func redact(in http.Header) http.Header {
	h := in.Clone()
	if h.Get("Authorization") != "" {
		h.Set("Authorization", "REDACTED")
	}
	return h
}

func handler(w http.ResponseWriter, r *http.Request) {
	logger.Infow("got request",
		"method", r.Method,
		"url", r.URL.String(),
		"header", redact(r.Header))

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "registry is read-only", http.StatusBadRequest)
		return
	}

	switch r.URL.Path {
	case "/v2", "/v2/":
		proxyV2(w, r)
	case "/token":
		proxyToken(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/v2") {
			proxy(w, r)
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

func proxyV2(w http.ResponseWriter, r *http.Request) {
	var url string
	if *gcr {
		url = "https://gcr.io/v2/"
	} else {
		url = "https://ghcr.io/v2/"
	}
	req, _ := http.NewRequest(r.Method, url, nil)

	logger.Infow("sending request",
		"method", r.Method,
		"url", r.URL.String(),
		"header", redact(r.Header))
	w.Header().Set("X-Redirected", r.URL.String())

	resp, err := http.DefaultClient.Do(req)
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
			if k == "Www-Authenticate" {
				log.Println("=== BEFORE: Www-Authenticate:", vv)
				if *gcr {
					// GCR's token endpoint is /v2/token, we want callers to hit us at /token.
					vv = strings.Replace(vv, `realm="https://gcr.io/v2/`, fmt.Sprintf(`realm="https://%s/`, r.Host), 1)
				} else {
					vv = strings.Replace(vv, `realm="https://ghcr.io/`, fmt.Sprintf(`realm="https://%s/`, r.Host), 1)
				}
				log.Println("=== CHANGED: Www-Authenticate:", vv)
			}
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		logger.Errorf("Error copying response body: %v", err)
	}
}

func proxyToken(w http.ResponseWriter, r *http.Request) {
	vals := r.URL.Query()
	if *prefix != "" {
		scope := vals.Get("scope")
		scope = strings.Replace(scope, *prefix+"/", "", 1)
		vals.Set("scope", scope)
	}
	if *repo != "" {
		scope := vals.Get("scope")
		scope = strings.Replace(scope, "repository:", "repository:"+*repo+"/", 1)
		vals.Set("scope", scope)
	}
	var url string
	if *gcr {
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

	resp, err := http.DefaultClient.Do(req)
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

func proxy(w http.ResponseWriter, r *http.Request) {
	var url string
	if *gcr {
		url = "https://gcr.io/v2/"
	} else {
		url = "https://ghcr.io/v2/"
	}
	if *repo != "" {
		url += *repo + "/"
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	if *prefix != "" && !strings.HasPrefix(path, *prefix+"/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path = strings.TrimPrefix(path, *prefix+"/")
	url += path + "?" + r.URL.Query().Encode()
	req, _ := http.NewRequest(r.Method, url, nil)
	req.Header = r.Header.Clone()

	// If the request is coming in without auth, get some auth.
	// This is useful for testing, but should never happen in real life.
	// Actually, containerd seems to make unauthenticated HEAD requests before
	// hitting /v2/, so this might be load-bearing.
	if req.Header.Get("Authorization") == "" {
		t, resp, err := getToken(r)
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

	resp, err := http.DefaultTransport.RoundTrip(req) // Transport doesn't follow redirects.
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
			if k == "Link" && strings.HasPrefix(vv, "</v2/"+*repo) {
				log.Println("=== BEFORE: Link:", vv)
				vv = "</v2" + strings.TrimPrefix(vv, "</v2/"+*repo)
				log.Println("=== CHANGED: Link:", vv)
			}
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// If it's a list request, rewrite the response so the name key matches the
	// user's requested repo, otherwise clients will repeatedly request the
	// first page looking for their repo's tags.
	if *repo != "" && strings.Contains(r.URL.Path, "/tags/list") {
		var lr listResponse
		if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
			logger.Errorf("Error decoding list response body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Println("=== BEFORE: Name:", lr.Name)
		lr.Name = strings.Replace(lr.Name, *repo+"/", "", 1)
		log.Println("=== CHANGED: Name:", lr.Name)
		if err := json.NewEncoder(w).Encode(lr); err != nil {
			logger.Errorf("Error encoding list response body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
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

func getToken(r *http.Request) (string, *http.Response, error) {
	parts := strings.Split(r.URL.Path, "/")
	parts = parts[2 : len(parts)-2]
	if *prefix != "" {
		if parts[0] == *prefix {
			parts = parts[1:]
		} else {
			return "", nil, fmt.Errorf("request path does not match prefix: %s", parts)
		}
	}
	if *repo != "" {
		parts = append([]string{*repo}, parts...)
	}
	var url string
	if *gcr {
		url = fmt.Sprintf("https://gcr.io/v2/token?scope=repository:%s:pull&service=gcr.io", strings.Join(parts, "/"))
	} else {
		url = fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull&service=ghcr.io", strings.Join(parts, "/"))
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(req) //nolint:gosec
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
