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
	"knative.dev/pkg/logging"
)

func redact(in http.Header) http.Header {
	h := in.Clone()
	if h.Get("Authorization") != "" {
		h.Set("Authorization", "REDACTED")
	}
	return h
}

func New() http.Handler {
	router := mux.NewRouter()

	router.HandleFunc("/v2", v2)
	router.HandleFunc("/v2/", v2)

	router.HandleFunc("/token", token)
	router.HandleFunc("/v2/{repo}/{rest:.*}", proxy)

	// Redirect any other path to cgr.dev directly.
	// Among other things this will redirect URLs like https://distroless.dev/static:latest
	// to https://cgr.dev/chainguard/static:latest, which will redirect to a useful place.
	// Besides that, any other URL will probably end up serving a 404 from cgr.dev.
	router.HandleFunc("/{rest:.*}", ghpage)

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

func v2(resp http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	logger := logging.FromContext(ctx)

	out, _ := http.NewRequest(req.Method, "https://cgr.dev/v2/", nil)

	logger.Infow("sending request",
		"method", req.Method,
		"url", req.URL.String(),
		"header", redact(req.Header))
	resp.Header().Set("X-Redirected", req.URL.String())

	back, err := http.DefaultClient.Do(out)
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
				vv = strings.Replace(vv, `://cgr.dev/`, fmt.Sprintf(`://%s/`, req.Host), 1)
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

func token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logging.FromContext(ctx)

	vals := r.URL.Query()

	scope := vals.Get("scope")
	scope = strings.Replace(scope, "repository:", "repository:chainguard/", 1)
	vals.Set("scope", scope)

	url := "https://cgr.dev/token?" + vals.Encode()
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
	ctx := r.Context()
	logger := logging.FromContext(ctx)

	repo := mux.Vars(r)["repo"]
	rest := mux.Vars(r)["rest"]

	url := fmt.Sprintf("https://cgr.dev/v2/chainguard/%s/%s", repo, rest)
	if query := r.URL.Query().Encode(); query != "" {
		url += "?" + query
	}
	req, _ := http.NewRequest(r.Method, url, nil)
	req.Header = r.Header.Clone()

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
			// List responses include a response header to support pagination, that looks like:
			//   Link: </v2/distroless/static/tags/list?n=100&last=blah>; rel="next">
			//
			// In order for the client to be able to use this link, we need to rewrite it to
			// point to the user's requested repo, not the upstream:
			//   Link: </v2[/prefix]/static/repo/tags/list?n=100&last=blah>; rel="next">
			if k == "Link" && strings.HasPrefix(vv, "</v2/chainguard") {
				log.Println("=== BEFORE: Link:", vv)
				rest := strings.TrimPrefix(vv, "</v2/chainguard")
				vv = "</v2" + rest
				log.Println("=== CHANGED: Link:", vv)
			}

			w.Header().Add(k, vv)
		}
	}

	// If it's a list request, rewrite the response so the name key matches the
	// user's requested repo, otherwise clients will repeatedly request the
	// first page looking for their repo's tags.
	if strings.Contains(r.URL.Path, "/tags/list") {
		var lr listResponse
		if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
			logger.Errorf("Error decoding list response body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Println("=== BEFORE: Name:", lr.Name)
		lr.Name = strings.Replace(lr.Name, "chainguard/", "", 1)
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
	// cgr.dev's blob responses should just be a 302 redirect to R2, but
	// just in case we get a "real" response we'd like to avoid paying
	// the egress cost to serve it.
	// Manifests will be served directly and we don't mind paying to proxy
	// them because they're small.
	if !strings.Contains(r.URL.Path, "/blobs/") {
		if _, err := io.Copy(w, resp.Body); err != nil {
			logger.Errorf("Error copying response body: %v", err)
		}
	}
}

type listResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func ghpage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logging.FromContext(ctx)
	url := fmt.Sprintf("https://cgr.dev/chainguard%s", r.URL.Path)
	logger.Infof("Redirecting %q to %q", r.URL, url)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}
