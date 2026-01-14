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

func TestPatchShellRC(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Create existing bashrc
	os.WriteFile(bashrc, []byte("# existing content\n"), 0644)

	err := PatchShellRC(bashrc, "/path/to/env.sh")
	if err != nil {
		t.Fatalf("PatchShellRC failed: %v", err)
	}

	content, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(content), "source \"/path/to/env.sh\"") {
		t.Error("Missing source line")
	}
	if !strings.Contains(string(content), "# existing content") {
		t.Error("Clobbered existing content")
	}
}

func TestPatchShellRCIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	os.WriteFile(bashrc, []byte("# existing\n"), 0644)

	PatchShellRC(bashrc, "/path/to/env.sh")
	PatchShellRC(bashrc, "/path/to/env.sh") // Second call

	content, _ := os.ReadFile(bashrc)
	count := strings.Count(string(content), "source \"/path/to/env.sh\"")
	if count != 1 {
		t.Errorf("Expected 1 source line, got %d", count)
	}
}

func TestPatchShellRCCreatesFileIfMissing(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Don't create the file - let PatchShellRC create it
	err := PatchShellRC(bashrc, "/path/to/env.sh")
	if err != nil {
		t.Fatalf("PatchShellRC failed: %v", err)
	}

	content, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatalf("File was not created: %v", err)
	}
	if !strings.Contains(string(content), "source \"/path/to/env.sh\"") {
		t.Error("Missing source line in newly created file")
	}
}

func TestPatchAllShells(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Create both rc files
	bashrc := filepath.Join(tmpDir, ".bashrc")
	zshrc := filepath.Join(tmpDir, ".zshrc")
	os.WriteFile(bashrc, []byte("# bash\n"), 0644)
	os.WriteFile(zshrc, []byte("# zsh\n"), 0644)

	err := PatchAllShells()
	if err != nil {
		t.Fatalf("PatchAllShells failed: %v", err)
	}

	// Check bashrc was patched
	bashContent, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(bashContent), "source") {
		t.Error("bashrc not patched")
	}
	if !strings.Contains(string(bashContent), "# bash") {
		t.Error("bashrc original content clobbered")
	}

	// Check zshrc was patched
	zshContent, _ := os.ReadFile(zshrc)
	if !strings.Contains(string(zshContent), "source") {
		t.Error("zshrc not patched")
	}
	if !strings.Contains(string(zshContent), "# zsh") {
		t.Error("zshrc original content clobbered")
	}
}

func TestPatchAllShellsOnlyPatchesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Only create bashrc, not zshrc
	bashrc := filepath.Join(tmpDir, ".bashrc")
	zshrc := filepath.Join(tmpDir, ".zshrc")
	os.WriteFile(bashrc, []byte("# bash\n"), 0644)

	err := PatchAllShells()
	if err != nil {
		t.Fatalf("PatchAllShells failed: %v", err)
	}

	// bashrc should be patched
	bashContent, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(bashContent), "source") {
		t.Error("bashrc not patched")
	}

	// zshrc should not exist
	if _, err := os.Stat(zshrc); err == nil {
		t.Error("zshrc was created but shouldn't have been")
	}
}
