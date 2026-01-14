#!/bin/sh
set -e

echo "Installing LLM Proxy..."

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

# Download URL (update for real releases)
VERSION="${LLM_PROXY_VERSION:-latest}"
URL="https://github.com/prime-radiant-inc/llm-proxy/releases/download/${VERSION}/llm-proxy-${OS}-${ARCH}"

echo "Downloading from $URL..."
curl -fsSL "$URL" -o /tmp/llm-proxy
chmod +x /tmp/llm-proxy

# Install binary
if [ -w /usr/local/bin ]; then
  echo "Installing to /usr/local/bin/llm-proxy"
  mv /tmp/llm-proxy /usr/local/bin/llm-proxy
else
  echo "Installing to ~/.local/bin/llm-proxy"
  mkdir -p "$HOME/.local/bin"
  mv /tmp/llm-proxy "$HOME/.local/bin/llm-proxy"
  export PATH="$HOME/.local/bin:$PATH"
fi

# Run setup
llm-proxy --setup

echo ""
echo "Installation complete!"
echo "Restart your shell to activate."
