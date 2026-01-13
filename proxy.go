// proxy.go
package main

import (
	"io"
	"net/http"
	"strings"
)

type Proxy struct {
	client *http.Client
}

func NewProxy() *Proxy {
	return &Proxy{
		client: &http.Client{},
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse the proxy URL
	provider, upstream, path, err := ParseProxyURL(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Determine scheme (use http for tests, https for real)
	scheme := "https"
	if isLocalhost(upstream) {
		scheme = "http"
	}

	// Build upstream URL
	upstreamURL := scheme + "://" + upstream + path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	// Create forwarded request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, "failed to create request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers
	copyHeaders(proxyReq.Header, r.Header)

	// Set host header
	proxyReq.Host = upstream

	// Make request to upstream
	resp, err := p.client.Do(proxyReq)
	if err != nil {
		http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	copyHeaders(w.Header(), resp.Header)

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)

	_ = provider // Will be used for session tracking later
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// isLocalhost checks if the host is localhost for determining http vs https scheme.
// Uses strings.HasPrefix for safety (avoids panics on short strings).
func isLocalhost(host string) bool {
	return strings.HasPrefix(host, "127.0.0.1") || strings.HasPrefix(host, "localhost")
}
