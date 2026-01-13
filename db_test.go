// db_test.go
package main

import (
	"path/filepath"
	"testing"
)

func TestDBCreate(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	db, err := NewSessionDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	// Verify tables exist by inserting a session
	err = db.CreateSession("test-session", "anthropic", "api.anthropic.com", "test-file.jsonl")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
}

func TestDBSessionLookup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	db, err := NewSessionDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	// Create a session
	db.CreateSession("session-1", "anthropic", "api.anthropic.com", "session-1.jsonl")

	// Update with fingerprint
	err = db.UpdateSessionFingerprint("session-1", 1, "fingerprint-abc")
	if err != nil {
		t.Fatalf("Failed to update fingerprint: %v", err)
	}

	// Look up by fingerprint
	session, seq, err := db.FindByFingerprint("fingerprint-abc")
	if err != nil {
		t.Fatalf("Failed to find by fingerprint: %v", err)
	}
	if session != "session-1" {
		t.Errorf("Expected session-1, got %s", session)
	}
	if seq != 1 {
		t.Errorf("Expected seq 1, got %d", seq)
	}
}

func TestDBFingerprintNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	db, _ := NewSessionDB(dbPath)
	defer db.Close()

	session, _, err := db.FindByFingerprint("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if session != "" {
		t.Error("Expected empty session for nonexistent fingerprint")
	}
}

func TestDBLatestFingerprint(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sessions.db")

	db, _ := NewSessionDB(dbPath)
	defer db.Close()

	db.CreateSession("session-1", "anthropic", "api.anthropic.com", "session-1.jsonl")
	db.UpdateSessionFingerprint("session-1", 1, "fp-1")
	db.UpdateSessionFingerprint("session-1", 2, "fp-2")
	db.UpdateSessionFingerprint("session-1", 3, "fp-3")

	// Get latest fingerprint for session
	fp, seq, err := db.GetLatestFingerprint("session-1")
	if err != nil {
		t.Fatalf("Failed to get latest: %v", err)
	}
	if fp != "fp-3" {
		t.Errorf("Expected fp-3, got %s", fp)
	}
	if seq != 3 {
		t.Errorf("Expected seq 3, got %d", seq)
	}
}
