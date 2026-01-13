// proxy_test.go
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyBasicRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected path /v1/messages, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"test":"data"}` {
			t.Errorf("unexpected body: %s", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"response":"ok"}`))
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	proxy := NewProxy()

	reqPath := "/anthropic/" + upstreamHost + "/v1/messages"
	req := httptest.NewRequest("POST", reqPath, strings.NewReader(`{"test":"data"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "sk-ant-test-key")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != `{"response":"ok"}` {
		t.Errorf("unexpected response: %s", w.Body.String())
	}
}

func TestProxyForwardsHeaders(t *testing.T) {
	var receivedHeaders http.Header

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")
	proxy := NewProxy()

	reqPath := "/anthropic/" + upstreamHost + "/v1/messages"
	req := httptest.NewRequest("POST", reqPath, nil)
	req.Header.Set("X-Api-Key", "sk-ant-test-key")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", "messages-2024-01-01")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if receivedHeaders.Get("X-Api-Key") != "sk-ant-test-key" {
		t.Error("X-Api-Key header not forwarded")
	}
	if receivedHeaders.Get("Anthropic-Version") != "2023-06-01" {
		t.Error("Anthropic-Version header not forwarded")
	}
}
