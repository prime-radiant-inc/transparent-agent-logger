// fingerprint_test.go
package main

import (
	"testing"
)

func TestFingerprintMessages(t *testing.T) {
	// Same messages should produce same fingerprint
	messages1 := `[{"role":"user","content":"hello"}]`
	messages2 := `[{"role":"user","content":"hello"}]`

	fp1 := FingerprintMessages([]byte(messages1))
	fp2 := FingerprintMessages([]byte(messages2))

	if fp1 != fp2 {
		t.Errorf("Same messages should produce same fingerprint: %s != %s", fp1, fp2)
	}
}

func TestFingerprintDifferentMessages(t *testing.T) {
	messages1 := `[{"role":"user","content":"hello"}]`
	messages2 := `[{"role":"user","content":"goodbye"}]`

	fp1 := FingerprintMessages([]byte(messages1))
	fp2 := FingerprintMessages([]byte(messages2))

	if fp1 == fp2 {
		t.Error("Different messages should produce different fingerprints")
	}
}

func TestFingerprintIgnoresWhitespace(t *testing.T) {
	// These are semantically equivalent JSON
	messages1 := `[{"role":"user","content":"hello"}]`
	messages2 := `[ { "role" : "user" , "content" : "hello" } ]`

	fp1 := FingerprintMessages([]byte(messages1))
	fp2 := FingerprintMessages([]byte(messages2))

	if fp1 != fp2 {
		t.Errorf("Whitespace differences should not affect fingerprint: %s != %s", fp1, fp2)
	}
}

func TestFingerprintKeyOrder(t *testing.T) {
	// Different key order should produce same fingerprint
	messages1 := `[{"role":"user","content":"hello"}]`
	messages2 := `[{"content":"hello","role":"user"}]`

	fp1 := FingerprintMessages([]byte(messages1))
	fp2 := FingerprintMessages([]byte(messages2))

	if fp1 != fp2 {
		t.Errorf("Key order should not affect fingerprint: %s != %s", fp1, fp2)
	}
}

func TestExtractMessagesFromRequest(t *testing.T) {
	// Anthropic request format
	request := `{"model":"claude-3","messages":[{"role":"user","content":"test"}],"max_tokens":100}`

	messages, err := ExtractMessages([]byte(request), "anthropic")
	if err != nil {
		t.Fatalf("Failed to extract messages: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
}

func TestExtractPriorMessages(t *testing.T) {
	// Should extract all but the last message for fingerprinting
	request := `{"model":"claude-3","messages":[
		{"role":"user","content":"first"},
		{"role":"assistant","content":"response"},
		{"role":"user","content":"second"}
	]}`

	prior, err := ExtractPriorMessages([]byte(request), "anthropic")
	if err != nil {
		t.Fatalf("Failed to extract prior: %v", err)
	}

	// Should only have first 2 messages
	if len(prior) != 2 {
		t.Errorf("Expected 2 prior messages, got %d", len(prior))
	}
}

func TestExtractAssistantMessageAnthropic(t *testing.T) {
	response := `{"content":[{"type":"text","text":"Hello there!"}],"model":"claude-3"}`

	msg, err := ExtractAssistantMessage([]byte(response), "anthropic")
	if err != nil {
		t.Fatalf("Failed to extract assistant message: %v", err)
	}

	if msg["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got %v", msg["role"])
	}

	// Content should be preserved as array (to match Claude Code follow-up format)
	content, ok := msg["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content to be array, got %T", msg["content"])
	}
	if len(content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(content))
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected content block to be map, got %T", content[0])
	}
	if block["text"] != "Hello there!" {
		t.Errorf("Expected text 'Hello there!', got %v", block["text"])
	}
}

func TestExtractAssistantMessageOpenAI(t *testing.T) {
	response := `{"choices":[{"message":{"role":"assistant","content":"Hi!"}}]}`

	msg, err := ExtractAssistantMessage([]byte(response), "openai")
	if err != nil {
		t.Fatalf("Failed to extract assistant message: %v", err)
	}

	if msg["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got %v", msg["role"])
	}
	if msg["content"] != "Hi!" {
		t.Errorf("Expected content 'Hi!', got %v", msg["content"])
	}
}

func TestExtractAssistantMessageMalformed(t *testing.T) {
	// Should return error for malformed JSON
	_, err := ExtractAssistantMessage([]byte("not json"), "anthropic")
	if err == nil {
		t.Error("Expected error for malformed JSON")
	}

	// Should return error for missing content
	_, err = ExtractAssistantMessage([]byte(`{}`), "anthropic")
	if err == nil {
		t.Error("Expected error for missing content")
	}
}

func TestExtractClientSessionID(t *testing.T) {
	// Claude Code format with _session_ marker
	request := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "hello"}],
		"metadata": {
			"user_id": "user_abc123_account_def456_session_550e8400-e29b-41d4-a716-446655440000"
		}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "anthropic")
	if sessionID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("Expected session ID '550e8400-e29b-41d4-a716-446655440000', got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDNoMarker(t *testing.T) {
	// No _session_ marker - should return empty
	request := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "hello"}],
		"metadata": {
			"user_id": "user_abc123_account_def456"
		}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "anthropic")
	if sessionID != "" {
		t.Errorf("Expected empty session ID when no marker, got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDNoMetadata(t *testing.T) {
	// No metadata field
	request := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "hello"}]
	}`

	sessionID := ExtractClientSessionID([]byte(request), "anthropic")
	if sessionID != "" {
		t.Errorf("Expected empty session ID when no metadata, got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDOpenAI(t *testing.T) {
	// OpenAI provider - not supported, should return empty
	request := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "hello"}],
		"metadata": {
			"user_id": "user_abc_session_12345"
		}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "openai")
	if sessionID != "" {
		t.Errorf("Expected empty session ID for OpenAI provider, got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDInvalidChars(t *testing.T) {
	// Session ID with invalid characters (path traversal attempt)
	request := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "hello"}],
		"metadata": {
			"user_id": "user_abc_session_../../../etc/passwd"
		}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "anthropic")
	if sessionID != "" {
		t.Errorf("Expected empty session ID for invalid chars, got '%s'", sessionID)
	}
}

func TestExtractClientSessionIDMultipleMarkers(t *testing.T) {
	// Multiple _session_ markers - should use the last one
	request := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "hello"}],
		"metadata": {
			"user_id": "user_session_old_session_new_session_final-uuid-123"
		}
	}`

	sessionID := ExtractClientSessionID([]byte(request), "anthropic")
	if sessionID != "final-uuid-123" {
		t.Errorf("Expected session ID 'final-uuid-123', got '%s'", sessionID)
	}
}

func TestIsValidSessionID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"abc123", true},
		{"ABC-123_xyz", true},
		{"", false},
		{"../etc/passwd", false},
		{"foo@bar", false},
		{"foo$bar", false},
		{"foo bar", false},
		{string(make([]byte, 256)), false}, // too long
	}

	for _, tt := range tests {
		got := isValidSessionID(tt.id)
		if got != tt.valid {
			t.Errorf("isValidSessionID(%q) = %v, want %v", tt.id, got, tt.valid)
		}
	}
}

