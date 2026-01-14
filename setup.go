// setup.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenerateEnvScript returns the content for the env.sh script that
// configures LLM clients to use the proxy. The script is POSIX-compliant
// for broad shell compatibility.
func GenerateEnvScript() string {
	return `# LLM Proxy environment configuration
# Source this from your shell rc file

_llm_proxy_port_file="$HOME/.local/state/llm-proxy/port"

if [ -f "$_llm_proxy_port_file" ]; then
    _llm_proxy_port=$(cat "$_llm_proxy_port_file")
    if curl -sf "http://localhost:$_llm_proxy_port/health" >/dev/null 2>&1; then
        export ANTHROPIC_BASE_URL="http://localhost:$_llm_proxy_port/anthropic/api.anthropic.com"
        export OPENAI_BASE_URL="http://localhost:$_llm_proxy_port/openai/api.openai.com"
    fi
fi
unset _llm_proxy_port_file _llm_proxy_port
`
}

// EnvScriptPath returns the standard location for the env.sh script.
// This follows XDG conventions: ~/.config/llm-proxy/env.sh
func EnvScriptPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "llm-proxy", "env.sh")
}

// WriteEnvScript writes the env.sh script to the standard location.
// It creates parent directories if they don't exist.
func WriteEnvScript() error {
	path := EnvScriptPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(GenerateEnvScript()), 0644)
}

// shellRCMarker is used to identify lines added by PatchShellRC
const shellRCMarker = "# LLM Proxy"

// PatchShellRC appends a source line to a shell rc file (e.g., .bashrc, .zshrc).
// It is idempotent - calling it multiple times will not add duplicate lines.
// If the file doesn't exist, it will be created.
func PatchShellRC(rcPath, envScriptPath string) error {
	content, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Already patched?
	if strings.Contains(string(content), shellRCMarker) {
		return nil
	}

	line := fmt.Sprintf("\n%s\nsource %q\n", shellRCMarker, envScriptPath)

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
	envScript := EnvScriptPath()

	shells := []string{".bashrc", ".zshrc"}
	for _, shell := range shells {
		rcPath := filepath.Join(home, shell)
		if _, err := os.Stat(rcPath); err == nil {
			if err := PatchShellRC(rcPath, envScript); err != nil {
				return err
			}
		}
	}
	return nil
}
