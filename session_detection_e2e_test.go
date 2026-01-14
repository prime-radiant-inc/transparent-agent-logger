// session_detection_e2e_test.go
// End-to-end tests for session detection behavior with real API calls
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSessionDetection_NoClientSessionID verifies that requests without
// client session IDs create separate sessions (no incorrect merging)
func TestSessionDetection_NoClientSessionID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live test in short mode")
	}

	apiKey := loadAPIKey(t)
	tmpDir := t.TempDir()

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	proxyURL := proxy.URL + "/anthropic/api.anthropic.com/v1/messages"
	client := &http.Client{}

	// Two identical requests without client session ID
	// Under the old fingerprint system, these would have been merged
	request := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say ALPHA"},
		},
	}

	// Request 1
	_ = makeRequest(t, client, proxyURL, apiKey, request)
	time.Sleep(100 * time.Millisecond)

	// Request 2 - identical content, no session ID
	_ = makeRequest(t, client, proxyURL, apiKey, request)
	time.Sleep(100 * time.Millisecond)

	// Verify we have 2 separate log files
	today := time.Now().Format("2006-01-02")
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "api.anthropic.com", today, "*.jsonl"))

	if len(logFiles) != 2 {
		t.Errorf("Expected 2 separate log files (no merging without session ID), got %d", len(logFiles))
		for _, f := range logFiles {
			data, _ := os.ReadFile(f)
			t.Logf("Log file %s:\n%s", filepath.Base(f), string(data))
		}
	}

	// Verify DB has 2 sessions
	db, _ := NewSessionDB(filepath.Join(tmpDir, "sessions.db"))
	defer db.Close()

	var count int
	row := db.db.QueryRow("SELECT COUNT(*) FROM sessions")
	row.Scan(&count)

	if count != 2 {
		t.Errorf("Expected 2 sessions in DB, got %d", count)
	}
}

// TestSessionDetection_SameClientSessionID verifies that requests with
// the same client session ID are grouped into the same session
func TestSessionDetection_SameClientSessionID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live test in short mode")
	}

	apiKey := loadAPIKey(t)
	tmpDir := t.TempDir()

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	proxyURL := proxy.URL + "/anthropic/api.anthropic.com/v1/messages"
	client := &http.Client{}

	sessionID := "test-session-" + time.Now().Format("150405")

	// Request 1 with session ID
	req1 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 20,
		"messages": []map[string]string{
			{"role": "user", "content": "Remember: CODE=BANANA. Say OK."},
		},
		"metadata": map[string]string{
			"user_id": "user_test_session_" + sessionID,
		},
	}

	resp1 := makeRequest(t, client, proxyURL, apiKey, req1)
	var result1 map[string]interface{}
	json.Unmarshal(resp1, &result1)
	content1 := extractTextContent(result1)
	t.Logf("Request 1 response: %s", content1)

	time.Sleep(100 * time.Millisecond)

	// Request 2 with SAME session ID - should continue
	req2 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 20,
		"messages": []interface{}{
			map[string]string{"role": "user", "content": "Remember: CODE=BANANA. Say OK."},
			map[string]interface{}{"role": "assistant", "content": content1},
			map[string]string{"role": "user", "content": "What is CODE?"},
		},
		"metadata": map[string]string{
			"user_id": "user_test_session_" + sessionID,
		},
	}

	resp2 := makeRequest(t, client, proxyURL, apiKey, req2)
	var result2 map[string]interface{}
	json.Unmarshal(resp2, &result2)
	content2 := extractTextContent(result2)
	t.Logf("Request 2 response: %s", content2)

	time.Sleep(100 * time.Millisecond)

	// Verify we have 1 log file (same session)
	today := time.Now().Format("2006-01-02")
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "api.anthropic.com", today, "*.jsonl"))

	if len(logFiles) != 1 {
		t.Errorf("Expected 1 log file (same session ID), got %d", len(logFiles))
		for _, f := range logFiles {
			data, _ := os.ReadFile(f)
			t.Logf("Log file %s:\n%s", filepath.Base(f), string(data))
		}
		return
	}

	// Verify the log file has seq 1 and seq 2
	data, _ := os.ReadFile(logFiles[0])
	logContent := string(data)

	if !strings.Contains(logContent, `"seq":1`) {
		t.Error("Log should contain seq:1")
	}
	if !strings.Contains(logContent, `"seq":2`) {
		t.Error("Log should contain seq:2")
	}

	t.Logf("Log file contents:\n%s", logContent)

	// Verify DB has 1 session with last_seq=2
	db, _ := NewSessionDB(filepath.Join(tmpDir, "sessions.db"))
	defer db.Close()

	var count int
	row := db.db.QueryRow("SELECT COUNT(*) FROM sessions")
	row.Scan(&count)

	if count != 1 {
		t.Errorf("Expected 1 session in DB, got %d", count)
	}

	var lastSeq int
	row = db.db.QueryRow("SELECT last_seq FROM sessions LIMIT 1")
	row.Scan(&lastSeq)

	if lastSeq != 2 {
		t.Errorf("Expected last_seq=2, got %d", lastSeq)
	}
}

// TestSessionDetection_DifferentClientSessionIDs verifies that requests
// with different client session IDs create separate sessions
func TestSessionDetection_DifferentClientSessionIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live test in short mode")
	}

	apiKey := loadAPIKey(t)
	tmpDir := t.TempDir()

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	proxyURL := proxy.URL + "/anthropic/api.anthropic.com/v1/messages"
	client := &http.Client{}

	ts := time.Now().Format("150405")

	// Request with session ID "AAA"
	req1 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say CHERRY"},
		},
		"metadata": map[string]string{
			"user_id": "user_test_session_AAA-" + ts,
		},
	}

	_ = makeRequest(t, client, proxyURL, apiKey, req1)
	time.Sleep(100 * time.Millisecond)

	// Request with session ID "BBB" - same message content, different session
	req2 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say CHERRY"},
		},
		"metadata": map[string]string{
			"user_id": "user_test_session_BBB-" + ts,
		},
	}

	_ = makeRequest(t, client, proxyURL, apiKey, req2)
	time.Sleep(100 * time.Millisecond)

	// Verify we have 2 separate log files
	today := time.Now().Format("2006-01-02")
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "api.anthropic.com", today, "*.jsonl"))

	if len(logFiles) != 2 {
		t.Errorf("Expected 2 log files (different session IDs), got %d", len(logFiles))
		for _, f := range logFiles {
			data, _ := os.ReadFile(f)
			t.Logf("Log file %s:\n%s", filepath.Base(f), string(data))
		}
	}

	// Verify DB has 2 sessions
	db, _ := NewSessionDB(filepath.Join(tmpDir, "sessions.db"))
	defer db.Close()

	var count int
	row := db.db.QueryRow("SELECT COUNT(*) FROM sessions")
	row.Scan(&count)

	if count != 2 {
		t.Errorf("Expected 2 sessions in DB, got %d", count)
	}
}

// TestSessionDetection_MixedScenario tests a mix of requests with and without session IDs
func TestSessionDetection_MixedScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live test in short mode")
	}

	apiKey := loadAPIKey(t)
	tmpDir := t.TempDir()

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	proxyURL := proxy.URL + "/anthropic/api.anthropic.com/v1/messages"
	client := &http.Client{}

	sessionID := "mixed-test-" + time.Now().Format("150405")

	// Request 1: No session ID
	req1 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say DELTA"},
		},
	}
	_ = makeRequest(t, client, proxyURL, apiKey, req1)
	time.Sleep(100 * time.Millisecond)

	// Request 2: With session ID (first in session)
	req2 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say DELTA"},
		},
		"metadata": map[string]string{
			"user_id": "user_test_session_" + sessionID,
		},
	}
	resp2 := makeRequest(t, client, proxyURL, apiKey, req2)
	var result2 map[string]interface{}
	json.Unmarshal(resp2, &result2)
	content2 := extractTextContent(result2)
	time.Sleep(100 * time.Millisecond)

	// Request 3: Same session ID (continuation)
	req3 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []interface{}{
			map[string]string{"role": "user", "content": "Say DELTA"},
			map[string]interface{}{"role": "assistant", "content": content2},
			map[string]string{"role": "user", "content": "Now say ECHO"},
		},
		"metadata": map[string]string{
			"user_id": "user_test_session_" + sessionID,
		},
	}
	_ = makeRequest(t, client, proxyURL, apiKey, req3)
	time.Sleep(100 * time.Millisecond)

	// Request 4: No session ID again
	req4 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 10,
		"messages": []map[string]string{
			{"role": "user", "content": "Say FOXTROT"},
		},
	}
	_ = makeRequest(t, client, proxyURL, apiKey, req4)
	time.Sleep(100 * time.Millisecond)

	// Expected: 3 log files
	// - 1 for request 1 (no session ID)
	// - 1 for requests 2+3 (same session ID, seq 1 and 2)
	// - 1 for request 4 (no session ID)

	today := time.Now().Format("2006-01-02")
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "api.anthropic.com", today, "*.jsonl"))

	if len(logFiles) != 3 {
		t.Errorf("Expected 3 log files, got %d", len(logFiles))
	}

	// Count total requests logged
	totalRequests := 0
	for _, f := range logFiles {
		data, _ := os.ReadFile(f)
		t.Logf("Log file %s:\n%s", filepath.Base(f), string(data))
		totalRequests += strings.Count(string(data), `"type":"request"`)
	}

	if totalRequests != 4 {
		t.Errorf("Expected 4 total requests logged, got %d", totalRequests)
	}

	// Verify DB has 3 sessions
	db, _ := NewSessionDB(filepath.Join(tmpDir, "sessions.db"))
	defer db.Close()

	var count int
	row := db.db.QueryRow("SELECT COUNT(*) FROM sessions")
	row.Scan(&count)

	if count != 3 {
		t.Errorf("Expected 3 sessions in DB, got %d", count)
	}

	// Find the session with client_session_id and verify it has last_seq=2
	var lastSeq int
	row = db.db.QueryRow("SELECT last_seq FROM sessions WHERE client_session_id IS NOT NULL AND client_session_id != ''")
	err = row.Scan(&lastSeq)
	if err != nil {
		t.Errorf("Failed to find session with client_session_id: %v", err)
	} else if lastSeq != 2 {
		t.Errorf("Session with client_session_id should have last_seq=2, got %d", lastSeq)
	}
}

// TestSessionDetection_ThreeRequestContinuation verifies that a 3-request
// conversation all lands in the same session with correct sequence numbers
func TestSessionDetection_ThreeRequestContinuation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live test in short mode")
	}

	apiKey := loadAPIKey(t)
	tmpDir := t.TempDir()

	srv, err := NewServer(Config{Port: 8080, LogDir: tmpDir})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	proxyURL := proxy.URL + "/anthropic/api.anthropic.com/v1/messages"
	client := &http.Client{}

	sessionID := "three-turn-" + time.Now().Format("150405")
	metadata := map[string]string{"user_id": "user_test_session_" + sessionID}

	// Turn 1
	req1 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 20,
		"messages":   []map[string]string{{"role": "user", "content": "X=1. Say OK."}},
		"metadata":   metadata,
	}
	resp1 := makeRequest(t, client, proxyURL, apiKey, req1)
	var r1 map[string]interface{}
	json.Unmarshal(resp1, &r1)
	c1 := extractTextContent(r1)
	time.Sleep(100 * time.Millisecond)

	// Turn 2
	req2 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 20,
		"messages": []interface{}{
			map[string]string{"role": "user", "content": "X=1. Say OK."},
			map[string]interface{}{"role": "assistant", "content": c1},
			map[string]string{"role": "user", "content": "Y=2. Say OK."},
		},
		"metadata": metadata,
	}
	resp2 := makeRequest(t, client, proxyURL, apiKey, req2)
	var r2 map[string]interface{}
	json.Unmarshal(resp2, &r2)
	c2 := extractTextContent(r2)
	time.Sleep(100 * time.Millisecond)

	// Turn 3
	req3 := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 20,
		"messages": []interface{}{
			map[string]string{"role": "user", "content": "X=1. Say OK."},
			map[string]interface{}{"role": "assistant", "content": c1},
			map[string]string{"role": "user", "content": "Y=2. Say OK."},
			map[string]interface{}{"role": "assistant", "content": c2},
			map[string]string{"role": "user", "content": "What is X+Y?"},
		},
		"metadata": metadata,
	}
	resp3 := makeRequest(t, client, proxyURL, apiKey, req3)
	var r3 map[string]interface{}
	json.Unmarshal(resp3, &r3)
	c3 := extractTextContent(r3)
	t.Logf("Final response: %s", c3)
	time.Sleep(100 * time.Millisecond)

	// Verify: 1 log file with seq 1, 2, 3
	today := time.Now().Format("2006-01-02")
	logFiles, _ := filepath.Glob(filepath.Join(tmpDir, "api.anthropic.com", today, "*.jsonl"))

	if len(logFiles) != 1 {
		t.Errorf("Expected 1 log file, got %d", len(logFiles))
		for _, f := range logFiles {
			data, _ := os.ReadFile(f)
			t.Logf("Log file %s:\n%s", filepath.Base(f), string(data))
		}
		return
	}

	data, _ := os.ReadFile(logFiles[0])
	logContent := string(data)

	for seq := 1; seq <= 3; seq++ {
		needle := `"seq":` + string(rune('0'+seq))
		if !strings.Contains(logContent, needle) {
			t.Errorf("Log should contain seq:%d", seq)
		}
	}

	t.Logf("Log file:\n%s", logContent)

	// Verify DB
	db, _ := NewSessionDB(filepath.Join(tmpDir, "sessions.db"))
	defer db.Close()

	var lastSeq int
	row := db.db.QueryRow("SELECT last_seq FROM sessions WHERE client_session_id LIKE ?", "%"+sessionID)
	row.Scan(&lastSeq)

	if lastSeq != 3 {
		t.Errorf("Expected last_seq=3, got %d", lastSeq)
	}
}

// makeRequestWithBody is a variant that allows raw body bytes
func makeRequestWithBody(t *testing.T, client *http.Client, url, apiKey string, bodyBytes []byte) []byte {
	t.Helper()

	req, _ := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody
}
