// urlparse.go
package main

import (
	"errors"
	"strings"
)

var (
	ErrInvalidProxyPath = errors.New("invalid proxy path: expected /{provider}/{upstream}/{path}")
	ErrUnknownProvider  = errors.New("unknown provider: must be 'anthropic' or 'openai'")
)

var validProviders = map[string]bool{
	"anthropic": true,
	"openai":    true,
}

// ParseProxyURL extracts provider, upstream host, and remaining path from a proxy URL.
// Expected format: /{provider}/{upstream}/{remaining_path}
func ParseProxyURL(urlPath string) (provider, upstream, path string, err error) {
	// Remove leading slash and split
	trimmed := strings.TrimPrefix(urlPath, "/")
	parts := strings.SplitN(trimmed, "/", 3)

	if len(parts) < 3 {
		return "", "", "", ErrInvalidProxyPath
	}

	provider = parts[0]
	upstream = parts[1]
	path = "/" + parts[2]

	if !validProviders[provider] {
		return "", "", "", ErrUnknownProvider
	}

	if upstream == "" {
		return "", "", "", ErrInvalidProxyPath
	}

	return provider, upstream, path, nil
}
