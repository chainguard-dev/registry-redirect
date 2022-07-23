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
	"io/ioutil"
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

	defer func() {
		if err := logger.Sync(); err != nil {
			log.Printf("error syncing logs: %v", err)
		}
	}()

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
	h.Set("Authorization", "REDACTED")
	return h
}

func handler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Printf("error syncing logs: %v", err)
		}
	}()

	logger.Infow("got request",
		"method", r.Method,
		"url", r.URL.String(),
		"header", redact(r.Header))

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "registry is read-only", http.StatusBadRequest)
		return
	}

	switch r.URL.Path {
	case "/v2/":
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

	logger.Info("sending request",
		"method", r.Method,
		"url", r.URL.String(),
		"header", redact(r.Header))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	logger.Info("got response",
		"method", r.Method,
		"url", r.URL.String(),
		"status", resp.Status,
		"header", redact(resp.Header))

	for k, v := range resp.Header {
		for _, vv := range v {
			if k == "Www-Authenticate" {
				if *gcr {
					// GCR's token endpoint is /v2/token, we want callers to hit us at /token.
					vv = strings.Replace(vv, `realm="https://gcr.io/v2/`, fmt.Sprintf(`realm="https://%s/`, r.Host), 1)
				} else {
					vv = strings.Replace(vv, `realm="https://ghcr.io/`, fmt.Sprintf(`realm="https://%s/`, r.Host), 1)
				}
				logger.Infof("CHANGED: Www-Authenticate: %s", vv)
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
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
	url += strings.TrimPrefix(r.URL.Path, "/v2/")
	req, _ := http.NewRequest(r.Method, url, nil)
	req.Header = r.Header.Clone()

	// If the request is coming in without auth, get some auth.
	// This is useful for testing, but should never happen in real life.
	if req.Header.Get("Authorization") == "" {
		t, err := getToken(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+t)
	}

	resp, err := http.DefaultTransport.RoundTrip(req) // Transport doesn't follow redirects.
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

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

func getToken(r *http.Request) (string, error) {
	parts := strings.Split(r.URL.Path, "/")
	parts = parts[2 : len(parts)-2]
	if *repo != "" {
		parts = append([]string{*repo}, parts...)
	}
	var url string
	if *gcr {
		url = fmt.Sprintf("https://gcr.io/v2/token?scope=repository:%s:pull&service=gcr.io", strings.Join(parts, "/"))
	} else {
		url = fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull&service=ghcr.io", strings.Join(parts, "/"))
	}
	req, _ := http.NewRequest(r.Method, url, nil)
	req.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(req) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		all, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("GET %s: %d %s", url, resp.StatusCode, string(all))
	}
	var t struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return "", err
	}
	return t.Token, nil
}
