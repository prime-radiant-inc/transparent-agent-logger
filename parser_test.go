package main

import (
	"testing"
)

func TestParseClaudeRequest(t *testing.T) {
	body := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 8096,
		"messages": [
			{"role": "user", "content": "What is 2+2?"}
		]
	}`

	parsed := ParseRequestBody(body, "api.anthropic.com")

	if parsed.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Expected claude-sonnet-4-20250514, got %s", parsed.Model)
	}

	if len(parsed.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(parsed.Messages))
	}

	if parsed.Messages[0].Role != "user" {
		t.Errorf("Expected role user, got %s", parsed.Messages[0].Role)
	}

	if parsed.Messages[0].TextContent != "What is 2+2?" {
		t.Errorf("Expected 'What is 2+2?', got %s", parsed.Messages[0].TextContent)
	}
}

func TestParseClaudeResponse(t *testing.T) {
	body := `{
		"content": [
			{"type": "text", "text": "2+2 equals 4."}
		],
		"usage": {"input_tokens": 10, "output_tokens": 8}
	}`

	parsed := ParseResponseBody(body, "api.anthropic.com")

	if len(parsed.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(parsed.Content))
	}

	if parsed.Content[0].Type != "text" {
		t.Errorf("Expected type text, got %s", parsed.Content[0].Type)
	}

	if parsed.Content[0].Text != "2+2 equals 4." {
		t.Errorf("Expected '2+2 equals 4.', got %s", parsed.Content[0].Text)
	}
}

func TestParseClaudeThinkingBlock(t *testing.T) {
	body := `{
		"content": [
			{"type": "thinking", "thinking": "Let me calculate this step by step..."},
			{"type": "text", "text": "The answer is 4."}
		]
	}`

	parsed := ParseResponseBody(body, "api.anthropic.com")

	if len(parsed.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(parsed.Content))
	}

	if parsed.Content[0].Type != "thinking" {
		t.Errorf("Expected thinking block first")
	}

	if parsed.Content[0].Thinking != "Let me calculate this step by step..." {
		t.Error("Thinking content not parsed correctly")
	}
}

func TestParseClaudeToolUse(t *testing.T) {
	body := `{
		"content": [
			{"type": "text", "text": "I'll read that file."},
			{"type": "tool_use", "id": "tool_123", "name": "Read", "input": {"path": "/tmp/test.txt"}}
		]
	}`

	parsed := ParseResponseBody(body, "api.anthropic.com")

	if len(parsed.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(parsed.Content))
	}

	toolBlock := parsed.Content[1]
	if toolBlock.Type != "tool_use" {
		t.Errorf("Expected tool_use, got %s", toolBlock.Type)
	}

	if toolBlock.ToolName != "Read" {
		t.Errorf("Expected tool name Read, got %s", toolBlock.ToolName)
	}
}

func TestParseToolResultWithIsError(t *testing.T) {
	// Request with tool_result containing is_error: true
	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "tool_result", "tool_use_id": "tool_123", "content": "Error: file not found", "is_error": true}
				]
			}
		]
	}`

	parsed := ParseRequestBody(body, "api.anthropic.com")

	if len(parsed.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(parsed.Messages))
	}

	if len(parsed.Messages[0].Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(parsed.Messages[0].Content))
	}

	toolResult := parsed.Messages[0].Content[0]
	if toolResult.Type != "tool_result" {
		t.Errorf("Expected tool_result type, got %s", toolResult.Type)
	}

	if toolResult.ToolID != "tool_123" {
		t.Errorf("Expected tool_use_id tool_123, got %s", toolResult.ToolID)
	}

	if !toolResult.IsError {
		t.Error("Expected IsError to be true for tool_result with is_error: true")
	}
}

func TestParseToolResultWithoutIsError(t *testing.T) {
	// Request with tool_result without is_error (success case)
	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "tool_result", "tool_use_id": "tool_456", "content": "File contents here"}
				]
			}
		]
	}`

	parsed := ParseRequestBody(body, "api.anthropic.com")

	toolResult := parsed.Messages[0].Content[0]
	if toolResult.IsError {
		t.Error("Expected IsError to be false when is_error is not present")
	}
}

func TestParseToolResultWithIsErrorFalse(t *testing.T) {
	// Request with explicit is_error: false
	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "tool_result", "tool_use_id": "tool_789", "content": "Success", "is_error": false}
				]
			}
		]
	}`

	parsed := ParseRequestBody(body, "api.anthropic.com")

	toolResult := parsed.Messages[0].Content[0]
	if toolResult.IsError {
		t.Error("Expected IsError to be false when is_error: false")
	}
}

func TestParseCacheTokens(t *testing.T) {
	// Response with cache token fields
	body := `{
		"content": [{"type": "text", "text": "Hello"}],
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_read_input_tokens": 80,
			"cache_creation_input_tokens": 20
		}
	}`

	parsed := ParseResponseBody(body, "api.anthropic.com")

	if parsed.Usage.InputTokens != 100 {
		t.Errorf("Expected input_tokens 100, got %d", parsed.Usage.InputTokens)
	}

	if parsed.Usage.OutputTokens != 50 {
		t.Errorf("Expected output_tokens 50, got %d", parsed.Usage.OutputTokens)
	}

	if parsed.Usage.CacheReadInputTokens != 80 {
		t.Errorf("Expected cache_read_input_tokens 80, got %d", parsed.Usage.CacheReadInputTokens)
	}

	if parsed.Usage.CacheCreationInputTokens != 20 {
		t.Errorf("Expected cache_creation_input_tokens 20, got %d", parsed.Usage.CacheCreationInputTokens)
	}
}

func TestParseCacheTokensMissing(t *testing.T) {
	// Response without cache token fields (should default to 0)
	body := `{
		"content": [{"type": "text", "text": "Hello"}],
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		}
	}`

	parsed := ParseResponseBody(body, "api.anthropic.com")

	if parsed.Usage.CacheReadInputTokens != 0 {
		t.Errorf("Expected cache_read_input_tokens to default to 0, got %d", parsed.Usage.CacheReadInputTokens)
	}

	if parsed.Usage.CacheCreationInputTokens != 0 {
		t.Errorf("Expected cache_creation_input_tokens to default to 0, got %d", parsed.Usage.CacheCreationInputTokens)
	}
}

func TestParseStreamingResponseCacheTokens(t *testing.T) {
	// Streaming response with cache tokens in message_start
	chunks := []StreamChunk{
		{Raw: `data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}}`},
		{Raw: `data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`},
		{Raw: `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{Raw: `data: {"type":"content_block_stop","index":0}`},
		{Raw: `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`},
	}

	parsed := ParseStreamingResponse(chunks)

	if parsed.Usage.InputTokens != 100 {
		t.Errorf("Expected input_tokens 100, got %d", parsed.Usage.InputTokens)
	}

	if parsed.Usage.CacheReadInputTokens != 80 {
		t.Errorf("Expected cache_read_input_tokens 80, got %d", parsed.Usage.CacheReadInputTokens)
	}

	if parsed.Usage.CacheCreationInputTokens != 20 {
		t.Errorf("Expected cache_creation_input_tokens 20, got %d", parsed.Usage.CacheCreationInputTokens)
	}

	if parsed.Usage.OutputTokens != 5 {
		t.Errorf("Expected output_tokens 5, got %d", parsed.Usage.OutputTokens)
	}
}

func TestParseStreamingResponseNoCacheTokens(t *testing.T) {
	// Streaming response without cache tokens (should default to 0)
	chunks := []StreamChunk{
		{Raw: `data: {"type":"message_start","message":{"usage":{"input_tokens":50}}}`},
		{Raw: `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`},
	}

	parsed := ParseStreamingResponse(chunks)

	if parsed.Usage.CacheReadInputTokens != 0 {
		t.Errorf("Expected cache_read_input_tokens to default to 0, got %d", parsed.Usage.CacheReadInputTokens)
	}

	if parsed.Usage.CacheCreationInputTokens != 0 {
		t.Errorf("Expected cache_creation_input_tokens to default to 0, got %d", parsed.Usage.CacheCreationInputTokens)
	}
}
