// setup.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// shellRCMarker is used to identify lines added by PatchShellRC
const shellRCMarker = "# LLM Proxy"

// PatchShellRC appends an eval line to a shell rc file (e.g., .bashrc, .zshrc).
// It is idempotent - calling it multiple times will not add duplicate lines.
// If the file doesn't exist, it will be created.
func PatchShellRC(rcPath string) error {
	content, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Already patched?
	if strings.Contains(string(content), shellRCMarker) {
		return nil
	}

	line := fmt.Sprintf("\n%s\neval \"$(llm-proxy --env)\"\n", shellRCMarker)

	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(line)
	return err
}

// UnpatchShellRC removes the LLM Proxy lines from a shell rc file.
// It removes the marker comment and the eval line that follows.
// If the file doesn't exist, it returns nil.
func UnpatchShellRC(rcPath string) error {
	content, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to do
		}
		return err
	}

	// If no marker, nothing to do
	if !strings.Contains(string(content), shellRCMarker) {
		return nil
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	skip := false
	for _, line := range lines {
		if strings.Contains(line, shellRCMarker) {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(line, "eval ") && strings.Contains(line, "llm-proxy") {
			skip = false
			continue
		}
		skip = false
		newLines = append(newLines, line)
	}

	return os.WriteFile(rcPath, []byte(strings.Join(newLines, "\n")), 0644)
}

// UnpatchAllShells removes LLM Proxy lines from all known shell rc files.
func UnpatchAllShells() error {
	home, _ := os.UserHomeDir()
	for _, shell := range []string{".bashrc", ".zshrc"} {
		if err := UnpatchShellRC(filepath.Join(home, shell)); err != nil {
			return err
		}
	}
	return nil
}

// PatchAllShells patches all known shell rc files in the user's home directory.
// It only patches shells that already have an rc file (doesn't create new ones).
func PatchAllShells() error {
	home, _ := os.UserHomeDir()

	shells := []string{".bashrc", ".zshrc"}
	for _, shell := range shells {
		rcPath := filepath.Join(home, shell)
		if _, err := os.Stat(rcPath); err == nil {
			if err := PatchShellRC(rcPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// GenerateSystemdUnit generates the systemd user service unit file content.
// The service runs as a user service (systemd --user), restarts on failure,
// and starts after default.target (login).
func GenerateSystemdUnit(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=LLM API Logging Proxy
After=default.target

[Service]
Type=simple
ExecStart=%s --service
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, binaryPath)
}

// SystemdServicePath returns the path for the systemd user service file.
func SystemdServicePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", "llm-proxy.service")
}

// InstallSystemdService creates the systemd user service file.
func InstallSystemdService(binaryPath string) error {
	path := SystemdServicePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	unit := GenerateSystemdUnit(binaryPath)
	return os.WriteFile(path, []byte(unit), 0644)
}

// Uninstall removes the LLM Proxy installation:
// - Stops and disables the systemd service (ignores errors if not installed)
// - Removes the service file
// - Removes shell RC patches
// - Removes the portfile
// - Preserves logs (user can delete manually if desired)
func Uninstall() error {
	// Stop and disable systemd service (ignore errors if not installed)
	exec.Command("systemctl", "--user", "stop", "llm-proxy").Run()
	exec.Command("systemctl", "--user", "disable", "llm-proxy").Run()

	// Remove service file
	os.Remove(SystemdServicePath())

	// Remove source lines from shell rc files
	if err := UnpatchAllShells(); err != nil {
		return err
	}

	// Remove portfile
	os.Remove(DefaultPortfilePath())

	fmt.Println("LLM Proxy uninstalled.")
	fmt.Println("Logs preserved at ~/.llm-provider-logs/")
	return nil
}

// FullSetup performs complete Linux installation:
// 1. Installs systemd user service
// 2. Enables and starts the service
// 3. Patches shell RC files
func FullSetup() error {
	// Find our binary path
	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}

	// Install systemd service
	if err := InstallSystemdService(binaryPath); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}

	// Enable and start service
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload failed: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "llm-proxy").Run(); err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "start", "llm-proxy").Run(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	// Setup shell
	if err := PatchAllShells(); err != nil {
		return err
	}

	return nil
}
