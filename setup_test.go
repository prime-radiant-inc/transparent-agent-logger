// setup_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateEnvScript(t *testing.T) {
	script := GenerateEnvScript()

	if !strings.Contains(script, "ANTHROPIC_BASE_URL") {
		t.Error("Missing ANTHROPIC_BASE_URL")
	}
	if !strings.Contains(script, "OPENAI_BASE_URL") {
		t.Error("Missing OPENAI_BASE_URL")
	}
	if !strings.Contains(script, ".local/state/llm-proxy/port") {
		t.Error("Missing portfile path")
	}
}

func TestGenerateEnvScriptIsPOSIXCompliant(t *testing.T) {
	script := GenerateEnvScript()

	// Check for common bashisms that should be avoided
	bashisms := []string{
		"[[",      // bash test syntax
		"]]",      // bash test syntax
		"$((", // bash arithmetic (though this one is actually POSIX)
		"function ", // bash function keyword
	}

	for _, bashism := range bashisms {
		if strings.Contains(script, bashism) {
			t.Errorf("Script contains bashism: %q", bashism)
		}
	}
}

func TestGenerateEnvScriptHealthCheck(t *testing.T) {
	script := GenerateEnvScript()

	// Should check proxy health before setting variables
	if !strings.Contains(script, "/health") {
		t.Error("Missing health check endpoint")
	}
	if !strings.Contains(script, "curl") {
		t.Error("Missing curl for health check")
	}
}

func TestGenerateEnvScriptCleansUpVariables(t *testing.T) {
	script := GenerateEnvScript()

	// Should clean up temporary variables
	if !strings.Contains(script, "unset") {
		t.Error("Missing unset to clean up temporary variables")
	}
}

func TestEnvScriptPath(t *testing.T) {
	path := EnvScriptPath()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not get home dir: %v", err)
	}

	expected := filepath.Join(home, ".config", "llm-proxy", "env.sh")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestWriteEnvScript(t *testing.T) {
	// Use a temp directory to avoid polluting user's config
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	err := WriteEnvScript()
	if err != nil {
		t.Fatalf("WriteEnvScript failed: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, ".config", "llm-proxy", "env.sh")

	// Check file exists
	info, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("env.sh not created: %v", err)
	}

	// Check file is not a directory
	if info.IsDir() {
		t.Error("env.sh is a directory, expected file")
	}

	// Check file has correct permissions (0644)
	if info.Mode().Perm() != 0644 {
		t.Errorf("expected permissions 0644, got %o", info.Mode().Perm())
	}

	// Check content matches GenerateEnvScript
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("could not read env.sh: %v", err)
	}

	expected := GenerateEnvScript()
	if string(content) != expected {
		t.Error("env.sh content does not match GenerateEnvScript output")
	}
}
