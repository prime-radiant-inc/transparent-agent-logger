// event_emission_test.go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// MockEventEmitter captures events for testing
type MockEventEmitter struct {
	TurnStartEvents  []MockTurnStartEvent
	TurnEndEvents    []MockTurnEndEvent
	ToolCallEvents   []MockToolCallEvent
	ToolResultEvents []MockToolResultEvent
}

type MockTurnStartEvent struct {
	SessionID      string
	Provider       string
	Machine        string
	TurnDepth      int
	ErrorRecovered bool
}

type MockTurnEndEvent struct {
	SessionID  string
	Provider   string
	Machine    string
	StopReason string
	IsRetry    bool
	ErrorType  string
	Patterns   PatternData
	Tokens     TokenData
}

type MockToolCallEvent struct {
	SessionID string
	Provider  string
	Machine   string
	ToolName  string
	ToolIndex int
	ToolUseID string
}

type MockToolResultEvent struct {
	SessionID string
	Provider  string
	Machine   string
	ToolName  string
	ToolUseID string
	IsError   bool
}

func (m *MockEventEmitter) EmitTurnStart(sessionID, provider, machine string, turnDepth int, errorRecovered bool) {
	m.TurnStartEvents = append(m.TurnStartEvents, MockTurnStartEvent{
		SessionID:      sessionID,
		Provider:       provider,
		Machine:        machine,
		TurnDepth:      turnDepth,
		ErrorRecovered: errorRecovered,
	})
}

func (m *MockEventEmitter) EmitTurnEnd(sessionID, provider, machine, stopReason string, isRetry bool, errorType string, patterns PatternData, tokens TokenData) {
	m.TurnEndEvents = append(m.TurnEndEvents, MockTurnEndEvent{
		SessionID:  sessionID,
		Provider:   provider,
		Machine:    machine,
		StopReason: stopReason,
		IsRetry:    isRetry,
		ErrorType:  errorType,
		Patterns:   patterns,
		Tokens:     tokens,
	})
}

func (m *MockEventEmitter) EmitToolCall(sessionID, provider, machine, toolName string, toolIndex int, toolUseID string) {
	m.ToolCallEvents = append(m.ToolCallEvents, MockToolCallEvent{
		SessionID: sessionID,
		Provider:  provider,
		Machine:   machine,
		ToolName:  toolName,
		ToolIndex: toolIndex,
		ToolUseID: toolUseID,
	})
}

func (m *MockEventEmitter) EmitToolResult(sessionID, provider, machine, toolName, toolUseID string, isError bool) {
	m.ToolResultEvents = append(m.ToolResultEvents, MockToolResultEvent{
		SessionID: sessionID,
		Provider:  provider,
		Machine:   machine,
		ToolName:  toolName,
		ToolUseID: toolUseID,
		IsError:   isError,
	})
}

func TestEventEmissionBasicTurn(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	// Create mock upstream that returns a simple response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello!"},
			},
			"usage": map[string]interface{}{
				"input_tokens":  10,
				"output_tokens": 5,
			},
			"stop_reason": "end_turn",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")

	// Make a request
	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-session-1"}
	}`

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Verify events
	if len(emitter.TurnStartEvents) != 1 {
		t.Fatalf("Expected 1 turn_start event, got %d", len(emitter.TurnStartEvents))
	}

	if emitter.TurnStartEvents[0].TurnDepth != 1 {
		t.Errorf("Expected turn_depth=1, got %d", emitter.TurnStartEvents[0].TurnDepth)
	}

	if emitter.TurnStartEvents[0].ErrorRecovered {
		t.Error("Expected error_recovered=false on first turn")
	}

	if len(emitter.TurnEndEvents) != 1 {
		t.Fatalf("Expected 1 turn_end event, got %d", len(emitter.TurnEndEvents))
	}

	if emitter.TurnEndEvents[0].StopReason != "end_turn" {
		t.Errorf("Expected stop_reason='end_turn', got %q", emitter.TurnEndEvents[0].StopReason)
	}

	if emitter.TurnEndEvents[0].Tokens.InputTokens != 10 {
		t.Errorf("Expected input_tokens=10, got %d", emitter.TurnEndEvents[0].Tokens.InputTokens)
	}

	if emitter.TurnEndEvents[0].Tokens.OutputTokens != 5 {
		t.Errorf("Expected output_tokens=5, got %d", emitter.TurnEndEvents[0].Tokens.OutputTokens)
	}
}

func TestEventEmissionWithToolCalls(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	// Create mock upstream that returns tool_use
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Let me check that file."},
				{"type": "tool_use", "id": "tool_123", "name": "Read", "input": map[string]string{"path": "/tmp/test.txt"}},
				{"type": "tool_use", "id": "tool_456", "name": "Bash", "input": map[string]string{"command": "ls"}},
			},
			"usage": map[string]interface{}{
				"input_tokens":  50,
				"output_tokens": 100,
			},
			"stop_reason": "tool_use",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")

	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Read /tmp/test.txt"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-session-2"}
	}`

	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")
	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Verify tool_call events
	if len(emitter.ToolCallEvents) != 2 {
		t.Fatalf("Expected 2 tool_call events, got %d", len(emitter.ToolCallEvents))
	}

	if emitter.ToolCallEvents[0].ToolName != "Read" {
		t.Errorf("Expected first tool 'Read', got %q", emitter.ToolCallEvents[0].ToolName)
	}
	if emitter.ToolCallEvents[0].ToolIndex != 0 {
		t.Errorf("Expected first tool index 0, got %d", emitter.ToolCallEvents[0].ToolIndex)
	}

	if emitter.ToolCallEvents[1].ToolName != "Bash" {
		t.Errorf("Expected second tool 'Bash', got %q", emitter.ToolCallEvents[1].ToolName)
	}
	if emitter.ToolCallEvents[1].ToolIndex != 1 {
		t.Errorf("Expected second tool index 1, got %d", emitter.ToolCallEvents[1].ToolIndex)
	}

	// Verify turn_end patterns
	if emitter.TurnEndEvents[0].Patterns.SessionToolCount != 2 {
		t.Errorf("Expected session_tool_count=2, got %d", emitter.TurnEndEvents[0].Patterns.SessionToolCount)
	}

	if emitter.TurnEndEvents[0].Patterns.ToolStreak != 1 {
		t.Errorf("Expected tool_streak=1 (first tool), got %d", emitter.TurnEndEvents[0].Patterns.ToolStreak)
	}
}

func TestEventEmissionWithToolResults(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		if requestCount == 1 {
			// First response: return tool_use to establish pending tool ID
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "tool_123", "name": "Read", "input": map[string]string{"path": "/tmp/test.txt"}},
				},
				"usage": map[string]interface{}{
					"input_tokens":  10,
					"output_tokens": 5,
				},
				"stop_reason": "tool_use",
			})
		} else {
			// Second response: after tool_result, return text
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "Done!"},
				},
				"usage": map[string]interface{}{
					"input_tokens":  10,
					"output_tokens": 5,
				},
				"stop_reason": "end_turn",
			})
		}
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// First request: get tool_use response which stores pending tool ID
	body1 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Read /tmp/test.txt"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-session-3"}
	}`

	req1 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")

	w1 := httptest.NewRecorder()
	proxy.ServeHTTP(w1, req1)

	// Verify tool_call was emitted
	if len(emitter.ToolCallEvents) != 1 {
		t.Fatalf("Expected 1 tool_call event after first request, got %d", len(emitter.ToolCallEvents))
	}

	// Second request: send tool_result
	body2 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Read /tmp/test.txt"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "tool_123", "name": "Read"}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tool_123", "content": "File contents here"}]}
		],
		"metadata": {"user_id": "user_abc_account_def_session_test-session-3"}
	}`

	req2 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")

	w2 := httptest.NewRecorder()
	proxy.ServeHTTP(w2, req2)

	// Verify tool_result event was emitted
	if len(emitter.ToolResultEvents) != 1 {
		t.Fatalf("Expected 1 tool_result event, got %d", len(emitter.ToolResultEvents))
	}

	if emitter.ToolResultEvents[0].ToolName != "Read" {
		t.Errorf("Expected tool_name='Read', got %q", emitter.ToolResultEvents[0].ToolName)
	}

	if emitter.ToolResultEvents[0].ToolUseID != "tool_123" {
		t.Errorf("Expected tool_use_id='tool_123', got %q", emitter.ToolResultEvents[0].ToolUseID)
	}

	if emitter.ToolResultEvents[0].IsError {
		t.Error("Expected is_error=false")
	}
}

func TestErrorRecoveredFlag(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		if requestCount == 1 {
			// First response: return tool_use
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "tool_err", "name": "Bash", "input": map[string]string{"command": "ls"}},
				},
				"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				"stop_reason": "tool_use",
			})
		} else if requestCount == 2 {
			// Second response: return another tool_use (after error)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "tool_retry", "name": "Bash", "input": map[string]string{"command": "ls -la"}},
				},
				"usage":       map[string]interface{}{"input_tokens": 15, "output_tokens": 10},
				"stop_reason": "tool_use",
			})
		} else {
			// Third response: final response
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content":     []map[string]interface{}{{"type": "text", "text": "Done!"}},
				"usage":       map[string]interface{}{"input_tokens": 20, "output_tokens": 5},
				"stop_reason": "end_turn",
			})
		}
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// First request: get tool_use
	body1 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Run a command"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-error-recovery"}
	}`

	req1 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	proxy.ServeHTTP(w1, req1)

	// Second request: send tool_result with is_error=true
	body2 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Run a command"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "tool_err", "name": "Bash"}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tool_err", "content": "Command failed", "is_error": true}]}
		],
		"metadata": {"user_id": "user_abc_account_def_session_test-error-recovery"}
	}`

	req2 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	proxy.ServeHTTP(w2, req2)

	// Third request: successful tool_result (error_recovered should be true)
	body3 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Run a command"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "tool_err", "name": "Bash"}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tool_err", "content": "Command failed", "is_error": true}]},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "tool_retry", "name": "Bash"}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tool_retry", "content": "Success!"}]}
		],
		"metadata": {"user_id": "user_abc_account_def_session_test-error-recovery"}
	}`

	req3 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	proxy.ServeHTTP(w3, req3)

	// Verify turn_start events
	if len(emitter.TurnStartEvents) != 3 {
		t.Fatalf("Expected 3 turn_start events, got %d", len(emitter.TurnStartEvents))
	}

	// First turn: no error recovery (first request)
	if emitter.TurnStartEvents[0].ErrorRecovered {
		t.Error("Turn 1: expected error_recovered=false on first turn")
	}

	// Second turn: no error recovery (previous turn was successful)
	if emitter.TurnStartEvents[1].ErrorRecovered {
		t.Error("Turn 2: expected error_recovered=false (error happened IN this turn, not before)")
	}

	// Third turn: error_recovered=true because previous turn had is_error=true
	if !emitter.TurnStartEvents[2].ErrorRecovered {
		t.Error("Turn 3: expected error_recovered=true (previous turn had is_error)")
	}

	// Also verify is_retry on turn_end (same tool after error = retry)
	if len(emitter.TurnEndEvents) != 3 {
		t.Fatalf("Expected 3 turn_end events, got %d", len(emitter.TurnEndEvents))
	}

	// Turn 2's response has same tool as turn 1 and turn 1's error was detected, so is_retry=true
	if !emitter.TurnEndEvents[1].IsRetry {
		t.Error("Turn 2: expected is_retry=true (same tool Bash after error)")
	}
}

func TestToolStreakTracking(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		// Always return Bash as first tool
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "tool_use", "id": "tool_" + string(rune('0'+callCount)), "name": "Bash", "input": map[string]string{"command": "ls"}},
			},
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"stop_reason": "tool_use",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// Make 3 consecutive requests with same first tool (Bash)
	for i := 0; i < 3; i++ {
		body := `{
			"model": "claude-sonnet-4-20250514",
			"messages": [{"role": "user", "content": "Run command"}],
			"metadata": {"user_id": "user_abc_account_def_session_test-session-streak"}
		}`

		req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
	}

	// Verify streaks increased
	if len(emitter.TurnEndEvents) != 3 {
		t.Fatalf("Expected 3 turn_end events, got %d", len(emitter.TurnEndEvents))
	}

	expectedStreaks := []int{1, 2, 3}
	for i, expected := range expectedStreaks {
		if emitter.TurnEndEvents[i].Patterns.ToolStreak != expected {
			t.Errorf("Turn %d: expected tool_streak=%d, got %d", i+1, expected, emitter.TurnEndEvents[i].Patterns.ToolStreak)
		}
	}
}

func TestComputePatterns(t *testing.T) {
	tests := []struct {
		name             string
		state            *PatternState
		firstToolName    string
		expectedIsRetry  bool
		expectedStreak   int
		expectedRetry    int
		expectedLastTool string
	}{
		{
			name: "first tool in session",
			state: &PatternState{
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "Bash",
			expectedIsRetry:  false,
			expectedStreak:   1,
			expectedRetry:    0,
			expectedLastTool: "Bash",
		},
		{
			name: "same tool continues streak",
			state: &PatternState{
				LastToolName:   "Bash",
				ToolStreak:     2,
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "Bash",
			expectedIsRetry:  false,
			expectedStreak:   3,
			expectedRetry:    0,
			expectedLastTool: "Bash",
		},
		{
			name: "different tool resets streak",
			state: &PatternState{
				LastToolName:   "Bash",
				ToolStreak:     5,
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "Read",
			expectedIsRetry:  false,
			expectedStreak:   1,
			expectedRetry:    0,
			expectedLastTool: "Read",
		},
		{
			name: "same tool after error is retry",
			state: &PatternState{
				LastToolName:   "Bash",
				ToolStreak:     1,
				LastWasError:   true,
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "Bash",
			expectedIsRetry:  true,
			expectedStreak:   2,
			expectedRetry:    1,
			expectedLastTool: "Bash",
		},
		{
			name: "different tool after error not retry",
			state: &PatternState{
				LastToolName:   "Bash",
				ToolStreak:     1,
				LastWasError:   true,
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "Read",
			expectedIsRetry:  false,
			expectedStreak:   1,
			expectedRetry:    0,
			expectedLastTool: "Read",
		},
		{
			name: "no tools resets streak",
			state: &PatternState{
				LastToolName:   "Bash",
				ToolStreak:     5,
				RetryCount:     2,
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "",
			expectedIsRetry:  false,
			expectedStreak:   0,
			expectedRetry:    0,
			expectedLastTool: "Bash", // unchanged
		},
		{
			name: "consecutive retries increment count",
			state: &PatternState{
				LastToolName:   "Bash",
				ToolStreak:     2,
				RetryCount:     1,
				LastWasError:   true,
				PendingToolIDs: make(map[string]string),
			},
			firstToolName:    "Bash",
			expectedIsRetry:  true,
			expectedStreak:   3,
			expectedRetry:    2,
			expectedLastTool: "Bash",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isRetry := ComputePatterns(tc.state, tc.firstToolName)

			if isRetry != tc.expectedIsRetry {
				t.Errorf("isRetry: expected %v, got %v", tc.expectedIsRetry, isRetry)
			}
			if tc.state.ToolStreak != tc.expectedStreak {
				t.Errorf("ToolStreak: expected %d, got %d", tc.expectedStreak, tc.state.ToolStreak)
			}
			if tc.state.RetryCount != tc.expectedRetry {
				t.Errorf("RetryCount: expected %d, got %d", tc.expectedRetry, tc.state.RetryCount)
			}
			if tc.firstToolName != "" && tc.state.LastToolName != tc.expectedLastTool {
				t.Errorf("LastToolName: expected %q, got %q", tc.expectedLastTool, tc.state.LastToolName)
			}
			// Note: LastWasError is NOT cleared by ComputePatterns - it's managed by the
			// request processing flow (processToolResultsAndEmitEvents). ComputePatterns
			// only reads it for retry detection.
		})
	}
}

func TestExtractToolResults(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tool_1", "content": "Success"},
				{"type": "tool_result", "tool_use_id": "tool_2", "content": "Error", "is_error": true}
			]}
		]
	}`)

	results := extractToolResults(body)

	if len(results) != 2 {
		t.Fatalf("Expected 2 tool results, got %d", len(results))
	}

	if results[0].ToolUseID != "tool_1" {
		t.Errorf("Expected tool_1, got %s", results[0].ToolUseID)
	}
	if results[0].IsError {
		t.Error("Expected first result to not be error")
	}

	if results[1].ToolUseID != "tool_2" {
		t.Errorf("Expected tool_2, got %s", results[1].ToolUseID)
	}
	if !results[1].IsError {
		t.Error("Expected second result to be error")
	}
}

func TestExtractToolCalls(t *testing.T) {
	content := []ContentBlock{
		{Type: "text", Text: "Some text"},
		{Type: "tool_use", ToolID: "tool_1", ToolName: "Read"},
		{Type: "tool_use", ToolID: "tool_2", ToolName: "Bash"},
		{Type: "text", Text: "More text"},
		{Type: "tool_use", ToolID: "tool_3", ToolName: "Edit"},
	}

	calls := extractToolCalls(content)

	if len(calls) != 3 {
		t.Fatalf("Expected 3 tool calls, got %d", len(calls))
	}

	expected := []struct {
		name  string
		id    string
		index int
	}{
		{"Read", "tool_1", 0},
		{"Bash", "tool_2", 1},
		{"Edit", "tool_3", 2},
	}

	for i, e := range expected {
		if calls[i].ToolName != e.name {
			t.Errorf("Call %d: expected name %q, got %q", i, e.name, calls[i].ToolName)
		}
		if calls[i].ToolID != e.id {
			t.Errorf("Call %d: expected ID %q, got %q", i, e.id, calls[i].ToolID)
		}
		if calls[i].ToolIndex != e.index {
			t.Errorf("Call %d: expected index %d, got %d", i, e.index, calls[i].ToolIndex)
		}
	}
}

func TestSessionPatternStatePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	// Create first DB instance
	db1, err := NewSessionDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}

	db1.CreateSession("session-1", "anthropic", "api.anthropic.com", "session-1.jsonl")

	state := &PatternState{
		TurnCount:        5,
		LastToolName:     "Bash",
		ToolStreak:       3,
		RetryCount:       1,
		SessionToolCount: 10,
		LastWasError:     true,
		PendingToolIDs:   map[string]string{"tool_1": "Read"},
	}

	if err := db1.UpdatePatternState("session-1", state); err != nil {
		t.Fatalf("Failed to update pattern state: %v", err)
	}

	db1.Close()

	// Create second DB instance (simulates restart)
	db2, err := NewSessionDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen DB: %v", err)
	}
	defer db2.Close()

	loaded, err := db2.LoadPatternState("session-1")
	if err != nil {
		t.Fatalf("Failed to load pattern state: %v", err)
	}

	if loaded.TurnCount != 5 {
		t.Errorf("TurnCount: expected 5, got %d", loaded.TurnCount)
	}
	if loaded.LastToolName != "Bash" {
		t.Errorf("LastToolName: expected 'Bash', got %q", loaded.LastToolName)
	}
	if loaded.ToolStreak != 3 {
		t.Errorf("ToolStreak: expected 3, got %d", loaded.ToolStreak)
	}
	if loaded.RetryCount != 1 {
		t.Errorf("RetryCount: expected 1, got %d", loaded.RetryCount)
	}
	if loaded.SessionToolCount != 10 {
		t.Errorf("SessionToolCount: expected 10, got %d", loaded.SessionToolCount)
	}
	if !loaded.LastWasError {
		t.Error("LastWasError: expected true")
	}
	if loaded.PendingToolIDs["tool_1"] != "Read" {
		t.Errorf("PendingToolIDs: expected tool_1=Read, got %v", loaded.PendingToolIDs)
	}
}

// TestClassifyErrorType_AllBranches tests all error type classification branches
func TestClassifyErrorType_AllBranches(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   string
	}{
		{"success 200", 200, "", ""},
		{"success 201", 201, "", ""},
		{"success 204", 204, "", ""},
		{"rate limit 429", 429, "", "rate_limit"},
		{"server error 500", 500, "", "server_error"},
		{"server error 502", 502, "", "server_error"},
		{"server error 503", 503, "", "server_error"},
		{"bad request 400 generic", 400, "invalid json", "invalid_request"},
		{"bad request 400 context length", 400, "context length exceeded", "context_length"},
		{"bad request 400 too long", 400, "request too long", "context_length"},
		{"unauthorized 401", 401, "", "invalid_request"},
		{"forbidden 403", 403, "", "invalid_request"},
		{"not found 404", 404, "", "invalid_request"},
		{"other 4xx 422", 422, "", "invalid_request"},
		{"unknown 300", 300, "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifyErrorType(tc.statusCode, tc.body)
			if result != tc.expected {
				t.Errorf("classifyErrorType(%d, %q) = %q, want %q", tc.statusCode, tc.body, result, tc.expected)
			}
		})
	}
}

// TestStreamingEventEmission tests event emission for streaming responses
func TestStreamingEventEmission(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	// Create mock upstream that returns SSE streaming response
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		// Simulate Anthropic SSE format with tool_use
		chunks := []string{
			`data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":50,"output_tokens":0}}}` + "\n\n",
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_stream_1","name":"Read","input":{}}}` + "\n\n",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/tmp/test.txt\"}"}}` + "\n\n",
			`data: {"type":"content_block_stop","index":0}` + "\n\n",
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":25}}` + "\n\n",
			`data: {"type":"message_stop"}` + "\n\n",
		}

		flusher, _ := w.(http.Flusher)
		for _, chunk := range chunks {
			w.Write([]byte(chunk))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Read a file"}],
		"stream": true,
		"metadata": {"user_id": "user_abc_account_def_session_test-streaming"}
	}`

	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Verify turn_start was emitted
	if len(emitter.TurnStartEvents) != 1 {
		t.Fatalf("Expected 1 turn_start event, got %d", len(emitter.TurnStartEvents))
	}

	// Verify turn_end was emitted
	if len(emitter.TurnEndEvents) != 1 {
		t.Fatalf("Expected 1 turn_end event, got %d", len(emitter.TurnEndEvents))
	}

	// Verify stop_reason from streaming
	if emitter.TurnEndEvents[0].StopReason != "tool_use" {
		t.Errorf("Expected stop_reason='tool_use', got %q", emitter.TurnEndEvents[0].StopReason)
	}
}

// TestCacheTokensInEvents tests that cache tokens are passed through to events
func TestCacheTokensInEvents(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello!"},
			},
			"usage": map[string]interface{}{
				"input_tokens":                100,
				"output_tokens":               50,
				"cache_read_input_tokens":     80,
				"cache_creation_input_tokens": 20,
			},
			"stop_reason": "end_turn",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-cache-tokens"}
	}`

	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if len(emitter.TurnEndEvents) != 1 {
		t.Fatalf("Expected 1 turn_end event, got %d", len(emitter.TurnEndEvents))
	}

	tokens := emitter.TurnEndEvents[0].Tokens
	if tokens.InputTokens != 100 {
		t.Errorf("Expected input_tokens=100, got %d", tokens.InputTokens)
	}
	if tokens.OutputTokens != 50 {
		t.Errorf("Expected output_tokens=50, got %d", tokens.OutputTokens)
	}
	if tokens.CacheReadInputTokens != 80 {
		t.Errorf("Expected cache_read_input_tokens=80, got %d", tokens.CacheReadInputTokens)
	}
	if tokens.CacheCreationInputTokens != 20 {
		t.Errorf("Expected cache_creation_input_tokens=20, got %d", tokens.CacheCreationInputTokens)
	}
}

// TestMultipleToolResultsInSingleRequest tests handling multiple tool_results in one request
func TestMultipleToolResultsInSingleRequest(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		if requestCount == 1 {
			// First response: return multiple tool_uses
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "tool_a", "name": "Read", "input": map[string]string{"path": "/a"}},
					{"type": "tool_use", "id": "tool_b", "name": "Bash", "input": map[string]string{"command": "ls"}},
					{"type": "tool_use", "id": "tool_c", "name": "Edit", "input": map[string]string{"file": "/c"}},
				},
				"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 20},
				"stop_reason": "tool_use",
			})
		} else {
			// Second response: after all tool_results
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content":     []map[string]interface{}{{"type": "text", "text": "All done!"}},
				"usage":       map[string]interface{}{"input_tokens": 50, "output_tokens": 10},
				"stop_reason": "end_turn",
			})
		}
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// First request: get multiple tool_uses
	body1 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Do multiple things"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-multi-tool"}
	}`

	req1 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	proxy.ServeHTTP(w1, req1)

	// Verify 3 tool_call events
	if len(emitter.ToolCallEvents) != 3 {
		t.Fatalf("Expected 3 tool_call events, got %d", len(emitter.ToolCallEvents))
	}

	// Second request: send all 3 tool_results (one with error)
	body2 := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Do multiple things"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "tool_a", "name": "Read"},
				{"type": "tool_use", "id": "tool_b", "name": "Bash"},
				{"type": "tool_use", "id": "tool_c", "name": "Edit"}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tool_a", "content": "File contents"},
				{"type": "tool_result", "tool_use_id": "tool_b", "content": "Command failed", "is_error": true},
				{"type": "tool_result", "tool_use_id": "tool_c", "content": "Edit successful"}
			]}
		],
		"metadata": {"user_id": "user_abc_account_def_session_test-multi-tool"}
	}`

	req2 := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	proxy.ServeHTTP(w2, req2)

	// Verify 3 tool_result events
	if len(emitter.ToolResultEvents) != 3 {
		t.Fatalf("Expected 3 tool_result events, got %d", len(emitter.ToolResultEvents))
	}

	// Check tool names were resolved
	expectedResults := []struct {
		toolName string
		isError  bool
	}{
		{"Read", false},
		{"Bash", true},
		{"Edit", false},
	}

	for i, expected := range expectedResults {
		if emitter.ToolResultEvents[i].ToolName != expected.toolName {
			t.Errorf("Result %d: expected tool_name=%q, got %q", i, expected.toolName, emitter.ToolResultEvents[i].ToolName)
		}
		if emitter.ToolResultEvents[i].IsError != expected.isError {
			t.Errorf("Result %d: expected is_error=%v, got %v", i, expected.isError, emitter.ToolResultEvents[i].IsError)
		}
	}

	// Verify session_tool_count accumulated correctly (3 tools from first response)
	if emitter.TurnEndEvents[0].Patterns.SessionToolCount != 3 {
		t.Errorf("Expected session_tool_count=3 after first turn, got %d", emitter.TurnEndEvents[0].Patterns.SessionToolCount)
	}
}

// TestUnknownToolUseID tests tool_result with unknown tool_use_id
func TestUnknownToolUseID(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content":     []map[string]interface{}{{"type": "text", "text": "Done"}},
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"stop_reason": "end_turn",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// Request with tool_result that has no matching pending tool_use
	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Something"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "tool_xyz", "name": "Mystery"}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tool_xyz", "content": "Result"}]}
		],
		"metadata": {"user_id": "user_abc_account_def_session_test-unknown-tool"}
	}`

	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Should emit tool_result with "unknown" name since tool_xyz wasn't in pending_tool_ids
	if len(emitter.ToolResultEvents) != 1 {
		t.Fatalf("Expected 1 tool_result event, got %d", len(emitter.ToolResultEvents))
	}

	if emitter.ToolResultEvents[0].ToolName != "unknown" {
		t.Errorf("Expected tool_name='unknown' for untracked tool_use_id, got %q", emitter.ToolResultEvents[0].ToolName)
	}
}

// TestEventEmissionWithErrorResponse tests event emission when upstream returns error
func TestEventEmissionWithErrorResponse(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "rate_limit_error",
				"message": "Rate limit exceeded",
			},
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-error-response"}
	}`

	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// turn_start should still be emitted
	if len(emitter.TurnStartEvents) != 1 {
		t.Fatalf("Expected 1 turn_start event even on error, got %d", len(emitter.TurnStartEvents))
	}

	// turn_end should be emitted with error_type
	if len(emitter.TurnEndEvents) != 1 {
		t.Fatalf("Expected 1 turn_end event, got %d", len(emitter.TurnEndEvents))
	}

	if emitter.TurnEndEvents[0].ErrorType != "rate_limit" {
		t.Errorf("Expected error_type='rate_limit', got %q", emitter.TurnEndEvents[0].ErrorType)
	}
}

// TestSessionToolCountAccumulates tests that session_tool_count accumulates across turns
func TestSessionToolCountAccumulates(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	toolCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		toolCount++
		w.Header().Set("Content-Type", "application/json")
		// Each response has 2 tools
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "tool_use", "id": "tool_" + string(rune('a'+toolCount*2)), "name": "Read", "input": map[string]string{}},
				{"type": "tool_use", "id": "tool_" + string(rune('b'+toolCount*2)), "name": "Bash", "input": map[string]string{}},
			},
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 10},
			"stop_reason": "tool_use",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// Make 3 requests, each returning 2 tools
	for i := 0; i < 3; i++ {
		body := `{
			"model": "claude-sonnet-4-20250514",
			"messages": [{"role": "user", "content": "Do things"}],
			"metadata": {"user_id": "user_abc_account_def_session_test-accumulate"}
		}`

		req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
	}

	// Verify session_tool_count accumulates: 2, 4, 6
	expectedCounts := []int{2, 4, 6}
	for i, expected := range expectedCounts {
		if emitter.TurnEndEvents[i].Patterns.SessionToolCount != expected {
			t.Errorf("Turn %d: expected session_tool_count=%d, got %d", i+1, expected, emitter.TurnEndEvents[i].Patterns.SessionToolCount)
		}
	}
}

// TestTurnDepthIncrementsCorrectly tests turn_depth increments each turn
func TestTurnDepthIncrementsCorrectly(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content":     []map[string]interface{}{{"type": "text", "text": "Hello"}},
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"stop_reason": "end_turn",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	// Make 5 requests
	for i := 0; i < 5; i++ {
		body := `{
			"model": "claude-sonnet-4-20250514",
			"messages": [{"role": "user", "content": "Hello"}],
			"metadata": {"user_id": "user_abc_account_def_session_test-turn-depth"}
		}`

		req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
	}

	// Verify turn_depth in turn_start: 1, 2, 3, 4, 5
	for i := 0; i < 5; i++ {
		expected := i + 1
		if emitter.TurnStartEvents[i].TurnDepth != expected {
			t.Errorf("Turn %d: expected turn_depth=%d in turn_start, got %d", i+1, expected, emitter.TurnStartEvents[i].TurnDepth)
		}
		if emitter.TurnEndEvents[i].Patterns.TurnDepth != expected {
			t.Errorf("Turn %d: expected turn_depth=%d in turn_end, got %d", i+1, expected, emitter.TurnEndEvents[i].Patterns.TurnDepth)
		}
	}
}

// TestNoEventsWithoutEmitter tests that no events are emitted when emitter is nil
func TestNoEventsWithoutEmitter(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "tool_use", "id": "tool_1", "name": "Read", "input": map[string]string{}},
			},
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"stop_reason": "tool_use",
		})
	}))
	defer upstream.Close()

	// Create proxy WITHOUT event emitter
	proxy := NewProxyWithSessionManagerAndLogger(logger, sm)
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	body := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"metadata": {"user_id": "user_abc_account_def_session_test-no-emitter"}
	}`

	req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Request should succeed (proxy still works)
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// No way to verify events weren't emitted since there's no emitter to check,
	// but this ensures the code path works without panicking
}

// TestDifferentToolResetsStreak tests that streak resets when tool changes
func TestDifferentToolResetsStreak(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	callCount := 0
	tools := []string{"Bash", "Bash", "Read", "Read", "Read", "Bash"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		toolName := tools[callCount]
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "tool_use", "id": "tool_" + string(rune('0'+callCount)), "name": toolName, "input": map[string]string{}},
			},
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"stop_reason": "tool_use",
		})
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	for i := 0; i < 6; i++ {
		body := `{
			"model": "claude-sonnet-4-20250514",
			"messages": [{"role": "user", "content": "Do something"}],
			"metadata": {"user_id": "user_abc_account_def_session_test-streak-reset"}
		}`

		req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
	}

	// Expected streaks: Bash(1), Bash(2), Read(1), Read(2), Read(3), Bash(1)
	expectedStreaks := []int{1, 2, 1, 2, 3, 1}
	for i, expected := range expectedStreaks {
		if emitter.TurnEndEvents[i].Patterns.ToolStreak != expected {
			t.Errorf("Turn %d (%s): expected tool_streak=%d, got %d",
				i+1, tools[i], expected, emitter.TurnEndEvents[i].Patterns.ToolStreak)
		}
	}
}

// TestNoToolsResetsStreak tests that streak resets when response has no tools
func TestNoToolsResetsStreak(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := NewLogger(tmpDir)
	defer logger.Close()

	sm, _ := NewSessionManager(tmpDir, logger)
	defer sm.Close()

	emitter := &MockEventEmitter{}

	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount <= 2 {
			// First two: return tools
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "tool_" + string(rune('0'+callCount)), "name": "Bash", "input": map[string]string{}},
				},
				"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				"stop_reason": "tool_use",
			})
		} else if callCount == 3 {
			// Third: no tools (end conversation)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content":     []map[string]interface{}{{"type": "text", "text": "Done!"}},
				"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				"stop_reason": "end_turn",
			})
		} else {
			// Fourth: tools again
			json.NewEncoder(w).Encode(map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "tool_use", "id": "tool_4", "name": "Bash", "input": map[string]string{}},
				},
				"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				"stop_reason": "tool_use",
			})
		}
	}))
	defer upstream.Close()

	proxy := NewProxyWithEventEmitter(logger, sm, emitter, "test-machine")
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	for i := 0; i < 4; i++ {
		body := `{
			"model": "claude-sonnet-4-20250514",
			"messages": [{"role": "user", "content": "Do something"}],
			"metadata": {"user_id": "user_abc_account_def_session_test-no-tools-reset"}
		}`

		req := httptest.NewRequest("POST", "/anthropic/"+upstreamHost+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
	}

	// Expected streaks: Bash(1), Bash(2), no-tools(0), Bash(1)
	expectedStreaks := []int{1, 2, 0, 1}
	for i, expected := range expectedStreaks {
		if emitter.TurnEndEvents[i].Patterns.ToolStreak != expected {
			t.Errorf("Turn %d: expected tool_streak=%d, got %d", i+1, expected, emitter.TurnEndEvents[i].Patterns.ToolStreak)
		}
	}
}
