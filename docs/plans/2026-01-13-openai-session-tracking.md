# OpenAI Session Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add OpenAI session ID extraction and conversation endpoint detection to the transparent logging proxy.

**Architecture:** Extend `ExtractClientSessionID()` to handle OpenAI's multiple session ID sources (headers, body fields, URL path). Update `isConversationEndpoint()` to recognize OpenAI conversation endpoints. Maintain fingerprint fallback for requests without explicit session IDs.

**Tech Stack:** Go, HTTP headers, JSON parsing, regex for URL path extraction

---

## Background

The proxy currently extracts Anthropic session IDs from `metadata.user_id`. OpenAI has multiple APIs with different session ID sources:

| API | Endpoint | Session ID Sources |
|-----|----------|-------------------|
| Chat Completions | `/v1/chat/completions` | `user`, `metadata.session_id`, headers |
| Responses API | `/v1/responses` | `conversation`, `previous_response_id`, `metadata.session_id` |
| Completions (legacy) | `/v1/completions` | `user`, headers |
| Threads (Assistants) | `/v1/threads/{id}/*` | Thread ID from URL path |

**Priority order for extraction:**
1. URL path thread ID (for `/v1/threads/{id}/...`)
2. Request body: `conversation` (Responses API)
3. Request body: `previous_response_id` (Responses API chaining)
4. Request body: `metadata.session_id`
5. Request header: `X-Session-ID`
6. Request header: `X-Client-Request-Id`
7. Request body: `user` field
8. Fingerprint fallback (existing)

---

## Task 1: Update isConversationEndpoint for OpenAI

**Files:**
- Modify: `proxy.go:201-203`
- Test: `proxy_test.go` (new file)

**Step 1: Write the failing test**

Create `proxy_test.go`:

```go
// proxy_test.go
package main

import "testing"

func TestIsConversationEndpoint(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		// Anthropic
		{"/v1/messages", true},

		// OpenAI conversation endpoints
		{"/v1/chat/completions", true},
		{"/v1/responses", true},
		{"/v1/completions", true},
		{"/v1/threads/thread_abc123/messages", true},
		{"/v1/threads/thread_abc123/runs", true},
		{"/v1/threads/thread_abc123/runs/run_xyz/steps", true},

		// Non-conversation endpoints (should NOT log)
		{"/v1/messages/count_tokens", false},
		{"/v1/models", false},
		{"/v1/embeddings", false},
		{"/v1/images/generations", false},
		{"/v1/audio/transcriptions", false},
		{"/v1/files", false},
		{"/v1/threads", false},           // Creating thread, not a conversation
		{"/v1/conversations", false},     // CRUD operations only
		{"/v1/assistants", false},
		{"/v1/vector_stores", false},
	}

	for _, tt := range tests {
		got := isConversationEndpoint(tt.path)
		if got != tt.expected {
			t.Errorf("isConversationEndpoint(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestIsConversationEndpoint -v`
Expected: FAIL - new endpoints not recognized

**Step 3: Write minimal implementation**

Update `proxy.go:201-203`:

```go
// isConversationEndpoint returns true for API endpoints that represent conversations
// (i.e., have messages that can be tracked for session continuity)
func isConversationEndpoint(path string) bool {
	// Anthropic
	if path == "/v1/messages" {
		return true
	}

	// OpenAI Chat/Completions
	if path == "/v1/chat/completions" || path == "/v1/completions" || path == "/v1/responses" {
		return true
	}

	// OpenAI Threads API - matches /v1/threads/{id}/messages or /v1/threads/{id}/runs[/...]
	if strings.HasPrefix(path, "/v1/threads/") {
		parts := strings.Split(path, "/")
		// /v1/threads/{id}/messages or /v1/threads/{id}/runs
		if len(parts) >= 5 && (parts[4] == "messages" || parts[4] == "runs") {
			return true
		}
	}

	return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestIsConversationEndpoint -v`
Expected: PASS

**Step 5: Commit**

```bash
git add proxy.go proxy_test.go && git commit -m "feat: add OpenAI conversation endpoint detection

Extend isConversationEndpoint() to recognize:
- /v1/chat/completions
- /v1/responses (new Responses API)
- /v1/completions (legacy)
- /v1/threads/{id}/messages (Assistants API)
- /v1/threads/{id}/runs (Assistants API)

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 2: Add ExtractOpenAISessionID function

**Files:**
- Modify: `fingerprint.go`
- Test: `fingerprint_test.go`

**Step 1: Write the failing tests**

Add to `fingerprint_test.go`:

```go
func TestExtractClientSessionIDOpenAIChatCompletionsUser(t *testing.T) {
	// OpenAI Chat Completions with user field
	request := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hello"}],
		"user": "user-12345"
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai")
	if sessionID != "user-12345" {
		t.Errorf("Expected session ID 'user-12345', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAIMetadata(t *testing.T) {
	// OpenAI with metadata.session_id (takes priority over user)
	request := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hello"}],
		"user": "user-12345",
		"metadata": {"session_id": "sess-abc-789"}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai")
	if sessionID != "sess-abc-789" {
		t.Errorf("Expected session ID 'sess-abc-789', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAIResponsesConversation(t *testing.T) {
	// OpenAI Responses API with conversation field (highest body priority)
	request := `{
		"model": "gpt-4o",
		"input": [{"role": "user", "content": "hello"}],
		"conversation": "conv_abc123",
		"metadata": {"session_id": "other-session"}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai")
	if sessionID != "conv_abc123" {
		t.Errorf("Expected session ID 'conv_abc123', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAIPreviousResponseID(t *testing.T) {
	// OpenAI Responses API with previous_response_id
	request := `{
		"model": "gpt-4o",
		"input": [{"role": "user", "content": "hello"}],
		"previous_response_id": "resp_xyz789"
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai")
	if sessionID != "resp_xyz789" {
		t.Errorf("Expected session ID 'resp_xyz789', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAINoSession(t *testing.T) {
	// OpenAI request with no session identifiers
	request := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hello"}]
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai")
	if sessionID != "" {
		t.Errorf("Expected empty session ID, got '%s'", sessionID)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestExtractClientSessionIDOpenAI -v`
Expected: FAIL - all return empty string

**Step 3: Write minimal implementation**

Update `ExtractClientSessionID` in `fingerprint.go`:

```go
// ExtractClientSessionID extracts a client-provided session ID from the request body.
// For Anthropic, this is found in metadata.user_id with format:
//
//	user_<hash>_account_<uuid>_session_<session-uuid>
//
// For OpenAI, priority order:
//  1. conversation (Responses API)
//  2. previous_response_id (Responses API chaining)
//  3. metadata.session_id
//  4. user field
//
// Returns empty string if no session ID is found.
func ExtractClientSessionID(body []byte, provider string) string {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}

	if provider == "anthropic" {
		return extractAnthropicSessionID(request)
	}

	if provider == "openai" {
		return extractOpenAISessionID(request)
	}

	return ""
}

// extractAnthropicSessionID extracts session ID from Anthropic's metadata.user_id
func extractAnthropicSessionID(request map[string]interface{}) string {
	metadata, ok := request["metadata"].(map[string]interface{})
	if !ok {
		return ""
	}

	userID, ok := metadata["user_id"].(string)
	if !ok || userID == "" {
		return ""
	}

	const sessionMarker = "_session_"
	idx := lastIndex(userID, sessionMarker)
	if idx == -1 {
		return ""
	}

	sessionID := userID[idx+len(sessionMarker):]
	if !isValidSessionID(sessionID) {
		return ""
	}

	return sessionID
}

// extractOpenAISessionID extracts session ID from OpenAI request fields
// Priority: conversation > previous_response_id > metadata.session_id > user
func extractOpenAISessionID(request map[string]interface{}) string {
	// 1. conversation (Responses API)
	if conv, ok := request["conversation"].(string); ok && conv != "" {
		if isValidSessionID(conv) {
			return conv
		}
	}

	// 2. previous_response_id (Responses API chaining)
	if prevResp, ok := request["previous_response_id"].(string); ok && prevResp != "" {
		if isValidSessionID(prevResp) {
			return prevResp
		}
	}

	// 3. metadata.session_id
	if metadata, ok := request["metadata"].(map[string]interface{}); ok {
		if sessID, ok := metadata["session_id"].(string); ok && sessID != "" {
			if isValidSessionID(sessID) {
				return sessID
			}
		}
	}

	// 4. user field
	if user, ok := request["user"].(string); ok && user != "" {
		if isValidSessionID(user) {
			return user
		}
	}

	return ""
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -run TestExtractClientSessionID -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add fingerprint.go fingerprint_test.go && git commit -m "feat: add OpenAI session ID extraction

Extract session IDs from OpenAI requests with priority:
1. conversation (Responses API)
2. previous_response_id (Responses API)
3. metadata.session_id
4. user field

Refactored to separate extractAnthropicSessionID and extractOpenAISessionID.

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 3: Add Header-based Session ID Extraction

**Files:**
- Modify: `fingerprint.go`
- Modify: `proxy.go`
- Modify: `session.go`
- Test: `fingerprint_test.go`

The current `ExtractClientSessionID` only takes body. We need to also pass headers for OpenAI's `X-Session-ID` and `X-Client-Request-Id` headers.

**Step 1: Write the failing test**

Add to `fingerprint_test.go`:

```go
func TestExtractClientSessionIDOpenAIHeaders(t *testing.T) {
	// Request body with no session ID
	request := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hello"}]
	}`

	headers := http.Header{}
	headers.Set("X-Session-ID", "header-session-123")

	sessionID := ExtractClientSessionID([]byte(request), "openai", headers)
	if sessionID != "header-session-123" {
		t.Errorf("Expected session ID 'header-session-123', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAIClientRequestId(t *testing.T) {
	// Request body with no session ID, using X-Client-Request-Id
	request := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hello"}]
	}`

	headers := http.Header{}
	headers.Set("X-Client-Request-Id", "client-req-456")

	sessionID := ExtractClientSessionID([]byte(request), "openai", headers)
	if sessionID != "client-req-456" {
		t.Errorf("Expected session ID 'client-req-456', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAIBodyOverHeader(t *testing.T) {
	// Body session_id takes priority over header
	request := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hello"}],
		"metadata": {"session_id": "body-session"}
	}`

	headers := http.Header{}
	headers.Set("X-Session-ID", "header-session")

	sessionID := ExtractClientSessionID([]byte(request), "openai", headers)
	if sessionID != "body-session" {
		t.Errorf("Expected session ID 'body-session' (body priority), got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDAnthropicIgnoresHeaders(t *testing.T) {
	// Anthropic should ignore headers (not part of their API)
	request := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "hello"}]
	}`

	headers := http.Header{}
	headers.Set("X-Session-ID", "header-session")

	sessionID := ExtractClientSessionID([]byte(request), "anthropic", headers)
	if sessionID != "" {
		t.Errorf("Anthropic should ignore headers, got '%s'", sessionID)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestExtractClientSessionIDOpenAI -v`
Expected: FAIL - function signature mismatch

**Step 3: Write minimal implementation**

Update `fingerprint.go` to accept optional headers:

```go
// ExtractClientSessionID extracts a client-provided session ID from the request.
// For Anthropic, this is found in metadata.user_id with format:
//
//	user_<hash>_account_<uuid>_session_<session-uuid>
//
// For OpenAI, priority order:
//  1. conversation (Responses API)
//  2. previous_response_id (Responses API chaining)
//  3. metadata.session_id
//  4. X-Session-ID header
//  5. X-Client-Request-Id header
//  6. user field
//
// Returns empty string if no session ID is found.
func ExtractClientSessionID(body []byte, provider string, headers http.Header) string {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}

	if provider == "anthropic" {
		return extractAnthropicSessionID(request)
	}

	if provider == "openai" {
		return extractOpenAISessionID(request, headers)
	}

	return ""
}

// extractOpenAISessionID extracts session ID from OpenAI request fields and headers
// Priority: conversation > previous_response_id > metadata.session_id > X-Session-ID > X-Client-Request-Id > user
func extractOpenAISessionID(request map[string]interface{}, headers http.Header) string {
	// 1. conversation (Responses API)
	if conv, ok := request["conversation"].(string); ok && conv != "" {
		if isValidSessionID(conv) {
			return conv
		}
	}

	// 2. previous_response_id (Responses API chaining)
	if prevResp, ok := request["previous_response_id"].(string); ok && prevResp != "" {
		if isValidSessionID(prevResp) {
			return prevResp
		}
	}

	// 3. metadata.session_id
	if metadata, ok := request["metadata"].(map[string]interface{}); ok {
		if sessID, ok := metadata["session_id"].(string); ok && sessID != "" {
			if isValidSessionID(sessID) {
				return sessID
			}
		}
	}

	// 4. X-Session-ID header
	if headers != nil {
		if sessID := headers.Get("X-Session-ID"); sessID != "" {
			if isValidSessionID(sessID) {
				return sessID
			}
		}

		// 5. X-Client-Request-Id header
		if clientReq := headers.Get("X-Client-Request-Id"); clientReq != "" {
			if isValidSessionID(clientReq) {
				return clientReq
			}
		}
	}

	// 6. user field
	if user, ok := request["user"].(string); ok && user != "" {
		if isValidSessionID(user) {
			return user
		}
	}

	return ""
}
```

Add import to `fingerprint.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)
```

**Step 4: Update callers**

Update `session.go:48` to pass headers (add headers parameter to `GetOrCreateSession`):

```go
func (sm *SessionManager) GetOrCreateSession(body []byte, provider, upstream string, headers http.Header) (string, int, bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	clientSessionID := ExtractClientSessionID(body, provider, headers)
	if clientSessionID != "" {
		return sm.getOrCreateByClientSessionID(clientSessionID, provider, upstream)
	}

	return sm.getOrCreateByFingerprint(body, provider, upstream)
}
```

Update `proxy.go:106` call site:

```go
sessionID, seq, isNewSession, err = p.sessionManager.GetOrCreateSession(reqBody, provider, upstream, r.Header)
```

**Step 5: Update all existing tests to pass nil headers for Anthropic**

Update existing tests in `fingerprint_test.go` to use the new signature:

```go
// Update all existing ExtractClientSessionID calls:
sessionID := ExtractClientSessionID([]byte(request), "anthropic", nil)
```

**Step 6: Run all tests to verify they pass**

Run: `go test -v`
Expected: All PASS

**Step 7: Commit**

```bash
git add fingerprint.go fingerprint_test.go session.go proxy.go && git commit -m "feat: add header-based session ID extraction for OpenAI

Add support for X-Session-ID and X-Client-Request-Id headers.
Updated priority order for OpenAI session extraction:
1. conversation
2. previous_response_id
3. metadata.session_id
4. X-Session-ID header
5. X-Client-Request-Id header
6. user field

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 4: Add URL Path Thread ID Extraction

**Files:**
- Modify: `fingerprint.go`
- Modify: `proxy.go`
- Modify: `session.go`
- Test: `fingerprint_test.go`

For OpenAI Threads API (`/v1/threads/{thread_id}/messages`), the session ID is in the URL path.

**Step 1: Write the failing test**

Add to `fingerprint_test.go`:

```go
func TestExtractThreadIDFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/v1/threads/thread_abc123/messages", "thread_abc123"},
		{"/v1/threads/thread_xyz789/runs", "thread_xyz789"},
		{"/v1/threads/thread_test/runs/run_123/steps", "thread_test"},
		{"/v1/threads", ""},                    // No thread ID
		{"/v1/chat/completions", ""},           // Not threads endpoint
		{"/v1/threads//messages", ""},          // Empty thread ID
		{"/v1/threads/../../etc/passwd/messages", ""}, // Path traversal
	}

	for _, tt := range tests {
		got := ExtractThreadIDFromPath(tt.path)
		if got != tt.expected {
			t.Errorf("ExtractThreadIDFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestExtractThreadIDFromPath -v`
Expected: FAIL - function doesn't exist

**Step 3: Write minimal implementation**

Add to `fingerprint.go`:

```go
// ExtractThreadIDFromPath extracts thread ID from OpenAI Threads API paths
// Path format: /v1/threads/{thread_id}/messages or /v1/threads/{thread_id}/runs
// Returns empty string if not a threads endpoint or no valid thread ID
func ExtractThreadIDFromPath(path string) string {
	if !strings.HasPrefix(path, "/v1/threads/") {
		return ""
	}

	parts := strings.Split(path, "/")
	// parts: ["", "v1", "threads", "{thread_id}", "messages|runs", ...]
	if len(parts) < 5 {
		return ""
	}

	threadID := parts[3]
	if threadID == "" {
		return ""
	}

	// Validate thread ID
	if !isValidSessionID(threadID) {
		return ""
	}

	return threadID
}
```

Add `"strings"` to imports in `fingerprint.go`.

**Step 4: Run test to verify it passes**

Run: `go test -run TestExtractThreadIDFromPath -v`
Expected: PASS

**Step 5: Integrate with session extraction**

Update signature in `fingerprint.go`:

```go
// ExtractClientSessionID extracts a client-provided session ID from the request.
// path is the URL path, used for OpenAI Threads API thread ID extraction.
func ExtractClientSessionID(body []byte, provider string, headers http.Header, path string) string {
	if provider == "openai" {
		// Check URL path first for thread ID
		if threadID := ExtractThreadIDFromPath(path); threadID != "" {
			return threadID
		}
	}

	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}

	if provider == "anthropic" {
		return extractAnthropicSessionID(request)
	}

	if provider == "openai" {
		return extractOpenAISessionID(request, headers)
	}

	return ""
}
```

Update `session.go`:

```go
func (sm *SessionManager) GetOrCreateSession(body []byte, provider, upstream string, headers http.Header, path string) (string, int, bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	clientSessionID := ExtractClientSessionID(body, provider, headers, path)
	// ... rest unchanged
}
```

Update `proxy.go:106`:

```go
sessionID, seq, isNewSession, err = p.sessionManager.GetOrCreateSession(reqBody, provider, upstream, r.Header, path)
```

**Step 6: Update all tests to use new signature**

Update all `ExtractClientSessionID` calls to pass path:

```go
sessionID := ExtractClientSessionID([]byte(request), "anthropic", nil, "")
sessionID := ExtractClientSessionID([]byte(request), "openai", headers, "/v1/chat/completions")
```

**Step 7: Add integration test for thread ID extraction**

Add to `fingerprint_test.go`:

```go
func TestExtractClientSessionIDOpenAIThreadPath(t *testing.T) {
	// Empty body - thread ID comes from path
	request := `{
		"model": "gpt-4o"
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai", nil, "/v1/threads/thread_abc123/messages")
	if sessionID != "thread_abc123" {
		t.Errorf("Expected session ID 'thread_abc123', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAIThreadPathPriority(t *testing.T) {
	// Thread ID in path takes priority over body fields
	request := `{
		"model": "gpt-4o",
		"user": "other-user"
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai", nil, "/v1/threads/thread_xyz/runs")
	if sessionID != "thread_xyz" {
		t.Errorf("Expected session ID 'thread_xyz' (path priority), got '%s'", sessionID)
	}
}
```

**Step 8: Run all tests**

Run: `go test -v`
Expected: All PASS

**Step 9: Commit**

```bash
git add fingerprint.go fingerprint_test.go session.go proxy.go && git commit -m "feat: add URL path thread ID extraction for OpenAI Threads API

Extract thread_id from /v1/threads/{thread_id}/messages and /runs paths.
Thread ID from path takes highest priority for session tracking.

Full OpenAI session ID priority:
1. URL path thread ID
2. conversation field
3. previous_response_id
4. metadata.session_id
5. X-Session-ID header
6. X-Client-Request-Id header
7. user field

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 5: Add Integration Tests for OpenAI Endpoints

**Files:**
- Modify: `integration_test.go`

**Step 1: Write the integration tests**

Add to `integration_test.go`:

```go
func TestProxyOpenAIChatCompletionsLogging(t *testing.T) {
	tmpDir := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello!"}}]}`))
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	// OpenAI Chat Completions request
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"user":"test-user-123"}`
	req := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	time.Sleep(50 * time.Millisecond)

	// Should create log file in openai directory
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "openai", "*.jsonl"))
	if len(logFiles) != 1 {
		t.Errorf("Expected 1 log file, got %d", len(logFiles))
	}
}

func TestProxyOpenAIResponsesAPILogging(t *testing.T) {
	tmpDir := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_abc123","output":[{"content":"Hi!"}]}`))
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	// OpenAI Responses API request with conversation field
	body := `{"model":"gpt-4o","input":[{"role":"user","content":"hello"}],"conversation":"conv_test123"}`
	req := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	time.Sleep(50 * time.Millisecond)

	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "openai", "*.jsonl"))
	if len(logFiles) != 1 {
		t.Errorf("Expected 1 log file, got %d", len(logFiles))
	}
}

func TestProxyOpenAISessionContinuation(t *testing.T) {
	tmpDir := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"response"}}]}`))
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	// First request
	body1 := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"user":"session-test"}`
	req1 := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/chat/completions", strings.NewReader(body1))
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)

	time.Sleep(50 * time.Millisecond)

	// Second request (same user = same session)
	body2 := `{"model":"gpt-4o","messages":[{"role":"user","content":"followup"}],"user":"session-test"}`
	req2 := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/chat/completions", strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	time.Sleep(50 * time.Millisecond)

	// Should have 1 log file (same session)
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "openai", "*.jsonl"))
	if len(logFiles) != 1 {
		t.Errorf("Expected 1 log file (same session), got %d", len(logFiles))
	}
}

func TestProxyOpenAIEmbeddingsSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	// Embeddings request - should NOT be logged
	body := `{"model":"text-embedding-ada-002","input":"hello"}`
	req := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/embeddings", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	time.Sleep(50 * time.Millisecond)

	// Should NOT create log files
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "openai", "*.jsonl"))
	if len(logFiles) != 0 {
		t.Errorf("Expected no log files for embeddings, got %d", len(logFiles))
	}
}

func TestProxyOpenAIHeaderSessionID(t *testing.T) {
	tmpDir := t.TempDir()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hi"}}]}`))
	}))
	defer upstream.Close()

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	// Request with X-Session-ID header
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Session-ID", "header-sess-abc")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	// Second request with same header session ID
	req2 := httptest.NewRequest("POST", "/openai/"+upstreamHost+"/v1/chat/completions", strings.NewReader(body))
	req2.Header.Set("X-Session-ID", "header-sess-abc")
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	time.Sleep(50 * time.Millisecond)

	// Should have 1 log file (same session via header)
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "openai", "*.jsonl"))
	if len(logFiles) != 1 {
		t.Errorf("Expected 1 log file (same session via header), got %d", len(logFiles))
	}
}
```

**Step 2: Run integration tests**

Run: `go test -run TestProxyOpenAI -v`
Expected: All PASS

**Step 3: Commit**

```bash
git add integration_test.go && git commit -m "test: add integration tests for OpenAI session tracking

Test coverage for:
- Chat Completions with user field
- Responses API with conversation field
- Session continuation via user field
- Embeddings endpoint skipped (non-conversation)
- X-Session-ID header session tracking

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 6: Run Full Test Suite and Verify

**Files:** None (verification only)

**Step 1: Run all tests**

Run: `go test -v ./...`
Expected: All PASS

**Step 2: Run the manual threading test**

Run: `./test-threading.sh` (if exists)
Expected: Verify Anthropic threading still works

**Step 3: Verify no regressions**

Check:
- Anthropic session tracking still works
- Non-conversation endpoints still skipped
- Fingerprint fallback still works when no session ID provided

**Step 4: Final commit (if any cleanup needed)**

```bash
git status
# If clean, done. If changes needed, commit with descriptive message.
```

---

## Summary

| Task | Description | Key Changes |
|------|-------------|-------------|
| 1 | Update `isConversationEndpoint` | Add OpenAI endpoints to logging filter |
| 2 | Add `extractOpenAISessionID` | Extract from body fields |
| 3 | Add header extraction | Support `X-Session-ID`, `X-Client-Request-Id` |
| 4 | Add URL path extraction | Support `/v1/threads/{id}/*` |
| 5 | Integration tests | End-to-end tests for OpenAI |
| 6 | Verify full suite | Ensure no regressions |

**Files modified:**
- `fingerprint.go` - Session ID extraction logic
- `proxy.go` - Conversation endpoint detection
- `session.go` - Pass headers/path to extraction
- `proxy_test.go` (new) - Unit tests for endpoint detection
- `fingerprint_test.go` - Unit tests for session extraction
- `integration_test.go` - End-to-end tests
