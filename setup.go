// setup.go
package main

import (
	"os"
	"path/filepath"
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
