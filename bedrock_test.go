package main

import (
	"bytes"
	"os"
	"testing"
)

// --- Step 3a: LimitedWriter and model ID validation ---

func TestLimitedWriter_BasicWrite(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, N: 100}

	n, err := lw.Write([]byte("hello"))
	if n != 5 || err != nil {
		t.Errorf("Write() = (%d, %v), want (5, nil)", n, err)
	}
	if buf.String() != "hello" {
		t.Errorf("buffer = %q, want %q", buf.String(), "hello")
	}
	if lw.Overflow {
		t.Error("Overflow should be false")
	}
}

func TestLimitedWriter_OverflowDiscardsEntireChunk(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, N: 10}

	// First write fits
	n, err := lw.Write([]byte("12345"))
	if n != 5 || err != nil {
		t.Fatalf("first Write() = (%d, %v)", n, err)
	}

	// Second write exceeds limit — entire chunk discarded
	n, err = lw.Write([]byte("1234567890"))
	if n != 10 || err != nil {
		t.Errorf("overflow Write() = (%d, %v), want (10, nil)", n, err)
	}
	if !lw.Overflow {
		t.Error("Overflow should be true after exceeding limit")
	}
	// Buffer should only contain first write
	if buf.String() != "12345" {
		t.Errorf("buffer = %q, want %q", buf.String(), "12345")
	}
}

func TestLimitedWriter_PostOverflowWritesDiscarded(t *testing.T) {
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, N: 5}

	lw.Write([]byte("1234567890")) // triggers overflow
	n, err := lw.Write([]byte("more data"))
	if n != 9 || err != nil {
		t.Errorf("post-overflow Write() = (%d, %v), want (9, nil)", n, err)
	}
	// Buffer should be empty (first write was too big, discarded)
	if buf.Len() != 0 {
		t.Errorf("buffer len = %d, want 0", buf.Len())
	}
}

func TestLimitedWriter_AlwaysReportsFullSuccess(t *testing.T) {
	// This is critical: io.TeeReader propagates Write errors to io.Copy.
	// LimitedWriter must NEVER return an error.
	var buf bytes.Buffer
	lw := &LimitedWriter{W: &buf, N: 0} // zero limit = immediate overflow

	n, err := lw.Write([]byte("data"))
	if n != 4 {
		t.Errorf("n = %d, want 4", n)
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestExtractModelID_Valid(t *testing.T) {
	tests := []struct {
		path    string
		wantID  string
		wantErr bool
	}{
		{"/model/us.anthropic.claude-sonnet-4-5-20250929-v2:0/invoke-with-response-stream", "us.anthropic.claude-sonnet-4-5-20250929-v2:0", false},
		{"/model/anthropic.claude-3-haiku-20240307-v1:0/invoke", "anthropic.claude-3-haiku-20240307-v1:0", false},
		{"/model/us.anthropic.claude-haiku-4-5-20251001-v1:0/invoke-with-response-stream", "us.anthropic.claude-haiku-4-5-20251001-v1:0", false},
		{"/model/simple-model/invoke", "simple-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			id, err := extractModelID(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractModelID(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if id != tt.wantID {
				t.Errorf("extractModelID(%q) = %q, want %q", tt.path, id, tt.wantID)
			}
		})
	}
}

func TestExtractModelID_Invalid(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"empty model", "/model//invoke"},
		{"url encoded chars", "/model/foo%23bar/invoke"},
		{"spaces", "/model/foo bar/invoke"},
		{"query string injection", "/model/foo?bar=baz/invoke"},
		{"special chars", "/model/foo@bar/invoke"},
		{"no suffix", "/model/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractModelID(tt.path)
			if err == nil {
				t.Errorf("extractModelID(%q) should fail", tt.path)
			}
		})
	}
}

// --- Step 3b: Eventstream decoder ---

func TestDecodeBedrockEventstream_RealFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock-eventstream.bin")
	if err != nil {
		t.Fatalf("Failed to read test fixture: %v", err)
	}

	chunks, err := decodeBedrockEventstream(data)
	if err != nil {
		t.Fatalf("decodeBedrockEventstream() error = %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// Each chunk should have "data: " prefix for parser compatibility
	for i, c := range chunks {
		if len(c.Raw) < 6 || c.Raw[:6] != "data: " {
			t.Errorf("chunk[%d].Raw should start with 'data: ', got prefix %q", i, c.Raw[:min(10, len(c.Raw))])
		}
	}

	// Verify we get expected Anthropic event types
	foundMessageStart := false
	foundMessageStop := false
	for _, c := range chunks {
		raw := c.Raw[6:] // strip "data: "
		if bytes.Contains([]byte(raw), []byte(`"type":"message_start"`)) {
			foundMessageStart = true
		}
		if bytes.Contains([]byte(raw), []byte(`"type":"message_stop"`)) {
			foundMessageStop = true
		}
	}
	if !foundMessageStart {
		t.Error("expected message_start event in decoded chunks")
	}
	if !foundMessageStop {
		t.Error("expected message_stop event in decoded chunks")
	}
}

func TestDecodeBedrockEventstream_EmptyInput(t *testing.T) {
	chunks, err := decodeBedrockEventstream(nil)
	if err != nil {
		t.Errorf("expected no error for nil input, got %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for nil input, got %d", len(chunks))
	}

	chunks, err = decodeBedrockEventstream([]byte{})
	if err != nil {
		t.Errorf("expected no error for empty input, got %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty input, got %d", len(chunks))
	}
}

func TestDecodeBedrockEventstream_TruncatedInput(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock-eventstream.bin")
	if err != nil {
		t.Fatalf("Failed to read test fixture: %v", err)
	}

	// Truncate to partial frame — should return any complete frames decoded
	// before the error, plus a non-nil error
	truncated := data[:len(data)/2]
	chunks, err := decodeBedrockEventstream(truncated)
	// Should get some chunks from complete frames before the truncation
	if len(chunks) == 0 && err == nil {
		t.Error("expected either chunks or error from truncated input")
	}
}
