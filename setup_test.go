// setup_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchShellRC(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Create existing bashrc
	os.WriteFile(bashrc, []byte("# existing content\n"), 0644)

	err := PatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("PatchShellRC failed: %v", err)
	}

	content, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(content), `eval "$(llm-proxy --env)"`) {
		t.Error("Missing eval line")
	}
	if !strings.Contains(string(content), "# existing content") {
		t.Error("Clobbered existing content")
	}
	if !strings.Contains(string(content), "# LLM Proxy") {
		t.Error("Missing marker comment")
	}
}

func TestPatchShellRCIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	os.WriteFile(bashrc, []byte("# existing\n"), 0644)

	PatchShellRC(bashrc)
	PatchShellRC(bashrc) // Second call

	content, _ := os.ReadFile(bashrc)
	count := strings.Count(string(content), `eval "$(llm-proxy --env)"`)
	if count != 1 {
		t.Errorf("Expected 1 eval line, got %d", count)
	}
}

func TestPatchShellRCCreatesFileIfMissing(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Don't create the file - let PatchShellRC create it
	err := PatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("PatchShellRC failed: %v", err)
	}

	content, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatalf("File was not created: %v", err)
	}
	if !strings.Contains(string(content), `eval "$(llm-proxy --env)"`) {
		t.Error("Missing eval line in newly created file")
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
	if !strings.Contains(string(bashContent), `eval "$(llm-proxy --env)"`) {
		t.Error("bashrc not patched")
	}
	if !strings.Contains(string(bashContent), "# bash") {
		t.Error("bashrc original content clobbered")
	}

	// Check zshrc was patched
	zshContent, _ := os.ReadFile(zshrc)
	if !strings.Contains(string(zshContent), `eval "$(llm-proxy --env)"`) {
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
	if !strings.Contains(string(bashContent), `eval "$(llm-proxy --env)"`) {
		t.Error("bashrc not patched")
	}

	// zshrc should not exist
	if _, err := os.Stat(zshrc); err == nil {
		t.Error("zshrc was created but shouldn't have been")
	}
}

func TestGenerateSystemdUnit(t *testing.T) {
	unit := GenerateSystemdUnit("/usr/local/bin/llm-proxy")

	if !strings.Contains(unit, "ExecStart=/usr/local/bin/llm-proxy --service") {
		t.Error("Missing ExecStart")
	}
	if !strings.Contains(unit, "[Install]") {
		t.Error("Missing Install section")
	}
	if !strings.Contains(unit, "[Unit]") {
		t.Error("Missing Unit section")
	}
	if !strings.Contains(unit, "[Service]") {
		t.Error("Missing Service section")
	}
	if !strings.Contains(unit, "WantedBy=default.target") {
		t.Error("Missing WantedBy")
	}
}

func TestSystemdServicePath(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	path := SystemdServicePath()
	expected := filepath.Join(tmpDir, ".config", "systemd", "user", "llm-proxy.service")
	if path != expected {
		t.Errorf("Expected %s, got %s", expected, path)
	}
}

func TestInstallSystemdService(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	err := InstallSystemdService("/usr/local/bin/llm-proxy")
	if err != nil {
		t.Fatalf("InstallSystemdService failed: %v", err)
	}

	// Check file was created
	path := SystemdServicePath()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Service file not created: %v", err)
	}

	// Verify content
	if !strings.Contains(string(content), "ExecStart=/usr/local/bin/llm-proxy --service") {
		t.Error("Missing ExecStart in installed service")
	}
}

func TestUnpatchShellRC(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Create a patched bashrc
	content := `# existing content
# LLM Proxy
eval "$(llm-proxy --env)"
# more content
`
	os.WriteFile(bashrc, []byte(content), 0644)

	err := UnpatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("UnpatchShellRC failed: %v", err)
	}

	result, _ := os.ReadFile(bashrc)
	if strings.Contains(string(result), "# LLM Proxy") {
		t.Error("Marker comment still present")
	}
	if strings.Contains(string(result), `eval "$(llm-proxy --env)"`) {
		t.Error("Eval line still present")
	}
	if !strings.Contains(string(result), "# existing content") {
		t.Error("Clobbered existing content")
	}
	if !strings.Contains(string(result), "# more content") {
		t.Error("Clobbered more content")
	}
}

func TestUnpatchShellRCNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// File doesn't exist, should return nil
	err := UnpatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("UnpatchShellRC should not fail on non-existent file: %v", err)
	}
}

func TestUnpatchShellRCNoPatchPresent(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Create a file without the patch
	content := "# just some content\nalias ls='ls -la'\n"
	os.WriteFile(bashrc, []byte(content), 0644)

	err := UnpatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("UnpatchShellRC failed: %v", err)
	}

	result, _ := os.ReadFile(bashrc)
	if string(result) != content {
		t.Errorf("Content was modified when it shouldn't have been.\nExpected: %q\nGot: %q", content, string(result))
	}
}

func TestUnpatchAllShells(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Create patched rc files
	bashrc := filepath.Join(tmpDir, ".bashrc")
	zshrc := filepath.Join(tmpDir, ".zshrc")

	bashContent := `# bash config
# LLM Proxy
eval "$(llm-proxy --env)"
alias ls='ls -la'
`
	zshContent := `# zsh config
# LLM Proxy
eval "$(llm-proxy --env)"
bindkey -v
`
	os.WriteFile(bashrc, []byte(bashContent), 0644)
	os.WriteFile(zshrc, []byte(zshContent), 0644)

	err := UnpatchAllShells()
	if err != nil {
		t.Fatalf("UnpatchAllShells failed: %v", err)
	}

	// Check bashrc was unpatched
	bashResult, _ := os.ReadFile(bashrc)
	if strings.Contains(string(bashResult), "# LLM Proxy") {
		t.Error("bashrc still has marker")
	}
	if strings.Contains(string(bashResult), `eval "$(llm-proxy --env)"`) {
		t.Error("bashrc still has eval")
	}
	if !strings.Contains(string(bashResult), "# bash config") {
		t.Error("bashrc lost original content")
	}

	// Check zshrc was unpatched
	zshResult, _ := os.ReadFile(zshrc)
	if strings.Contains(string(zshResult), "# LLM Proxy") {
		t.Error("zshrc still has marker")
	}
	if strings.Contains(string(zshResult), `eval "$(llm-proxy --env)"`) {
		t.Error("zshrc still has eval")
	}
	if !strings.Contains(string(zshResult), "# zsh config") {
		t.Error("zshrc lost original content")
	}
}

func TestPatchAndUnpatchRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	bashrc := filepath.Join(tmpDir, ".bashrc")

	// Start with original content
	original := "# my config\nalias foo='bar'\n"
	os.WriteFile(bashrc, []byte(original), 0644)

	// Patch
	err := PatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("PatchShellRC failed: %v", err)
	}

	// Verify patched
	patched, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(patched), "# LLM Proxy") {
		t.Error("Patch didn't add marker")
	}

	// Unpatch
	err = UnpatchShellRC(bashrc)
	if err != nil {
		t.Fatalf("UnpatchShellRC failed: %v", err)
	}

	// Verify unpatched - should be back to original (with possible trailing newline differences)
	unpatched, _ := os.ReadFile(bashrc)
	if strings.Contains(string(unpatched), "# LLM Proxy") {
		t.Error("Unpatch didn't remove marker")
	}
	if strings.Contains(string(unpatched), `eval "$(llm-proxy --env)"`) {
		t.Error("Unpatch didn't remove eval")
	}
	if !strings.Contains(string(unpatched), "# my config") {
		t.Error("Original content was lost")
	}
	if !strings.Contains(string(unpatched), "alias foo='bar'") {
		t.Error("Original content was lost")
	}
}

func TestFullSetup(t *testing.T) {
	// This test verifies FullSetup without actually running systemctl
	// We test the parts we can test (service file creation, shell patching)
	// The systemctl commands would fail in test environment anyway

	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Create shell rc files so they get patched
	bashrc := filepath.Join(tmpDir, ".bashrc")
	zshrc := filepath.Join(tmpDir, ".zshrc")
	os.WriteFile(bashrc, []byte("# bash\n"), 0644)
	os.WriteFile(zshrc, []byte("# zsh\n"), 0644)

	// Test InstallSystemdService directly (part of FullSetup)
	binaryPath := "/usr/local/bin/llm-proxy"
	if err := InstallSystemdService(binaryPath); err != nil {
		t.Fatalf("InstallSystemdService failed: %v", err)
	}

	// Verify systemd service file was created
	servicePath := SystemdServicePath()
	content, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("Service file not created: %v", err)
	}
	if !strings.Contains(string(content), "ExecStart=/usr/local/bin/llm-proxy --service") {
		t.Error("Service file missing ExecStart")
	}

	// Test PatchAllShells (part of FullSetup)
	if err := PatchAllShells(); err != nil {
		t.Fatalf("PatchAllShells failed: %v", err)
	}

	// Verify shells were patched
	bashContent, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(bashContent), `eval "$(llm-proxy --env)"`) {
		t.Error("bashrc not patched")
	}
	zshContent, _ := os.ReadFile(zshrc)
	if !strings.Contains(string(zshContent), `eval "$(llm-proxy --env)"`) {
		t.Error("zshrc not patched")
	}
}
