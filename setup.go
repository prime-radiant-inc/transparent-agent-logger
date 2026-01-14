// setup.go
package main

import (
	"fmt"
	"os"
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
