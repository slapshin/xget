// Package redact masks credentials so they can be printed in logs and error
// messages without disclosing them.
package redact

import (
	"net/url"
	"strings"
)

// Secret hides a secret, keeping the last 4 chars when length allows.
func Secret(secret string) string {
	if len(secret) > 4 {
		return "****" + secret[len(secret)-4:]
	}

	return "***"
}

// URL masks any user:password embedded in the URL.
// The original string is returned when it has no userinfo or cannot be parsed.
func URL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	if parsed.User == nil {
		return raw
	}

	// Build the masked userinfo manually: url.User would percent-encode the
	// mask characters, so reconstruct around the authority marker instead.
	masked := Secret(parsed.User.String())
	parsed.User = nil

	// rest has the credentials stripped, so it is safe to return as-is if the
	// authority marker is somehow absent.
	rest := parsed.String()

	idx := strings.Index(rest, "//")
	if idx == -1 {
		return rest
	}

	return rest[:idx+2] + masked + "@" + rest[idx+2:]
}
