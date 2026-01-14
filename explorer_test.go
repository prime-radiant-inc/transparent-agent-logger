package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplorerServerHealth(t *testing.T) {
	tmpDir := t.TempDir()

	explorer := NewExplorer(tmpDir)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	explorer.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestExplorerListsSessions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake session log
	sessionDir := filepath.Join(tmpDir, "api.anthropic.com", "2026-01-14")
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(
		filepath.Join(sessionDir, "test-session.jsonl"),
		[]byte(`{"type":"session_start","_meta":{"ts":"2026-01-14T10:00:00Z","host":"api.anthropic.com"}}`),
		0644,
	)

	explorer := NewExplorer(tmpDir)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	explorer.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "test-session") {
		t.Error("Expected session list to contain test-session")
	}
}

func TestSessionInfoIncludesMessageCount(t *testing.T) {
	tmpDir := t.TempDir()

	// Create session with multiple entries
	sessionDir := filepath.Join(tmpDir, "api.anthropic.com", "2026-01-14")
	os.MkdirAll(sessionDir, 0755)

	content := `{"type":"session_start","_meta":{"ts":"2026-01-14T10:00:00Z"}}
{"type":"request","seq":1,"_meta":{"ts":"2026-01-14T10:00:01Z"}}
{"type":"response","seq":1,"_meta":{"ts":"2026-01-14T10:00:02Z"}}
{"type":"request","seq":2,"_meta":{"ts":"2026-01-14T10:05:00Z"}}
{"type":"response","seq":2,"_meta":{"ts":"2026-01-14T10:05:30Z"}}
`
	os.WriteFile(filepath.Join(sessionDir, "test-session.jsonl"), []byte(content), 0644)

	explorer := NewExplorer(tmpDir)
	sessions := explorer.listSessions()

	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	if sessions[0].MessageCount != 2 {
		t.Errorf("Expected 2 messages, got %d", sessions[0].MessageCount)
	}

	if sessions[0].TimeRange == "" {
		t.Error("Expected non-empty time range")
	}
}

func TestSessionDetailShowsEntries(t *testing.T) {
	tmpDir := t.TempDir()

	sessionDir := filepath.Join(tmpDir, "api.anthropic.com", "2026-01-14")
	os.MkdirAll(sessionDir, 0755)

	content := `{"type":"session_start","_meta":{"ts":"2026-01-14T10:00:00Z","host":"api.anthropic.com","session":"abc123"}}
{"type":"request","seq":1,"body":"{\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}","_meta":{"ts":"2026-01-14T10:00:01Z"}}
{"type":"response","seq":1,"body":"{\"content\":[{\"type\":\"text\",\"text\":\"Hi there!\"}]}","_meta":{"ts":"2026-01-14T10:00:02Z"}}
`
	os.WriteFile(filepath.Join(sessionDir, "abc123.jsonl"), []byte(content), 0644)

	explorer := NewExplorer(tmpDir)

	req := httptest.NewRequest("GET", "/session/abc123", nil)
	w := httptest.NewRecorder()

	explorer.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Hello") {
		t.Error("Expected session detail to show user message")
	}
	if !strings.Contains(body, "Hi there") {
		t.Error("Expected session detail to show assistant response")
	}
}
