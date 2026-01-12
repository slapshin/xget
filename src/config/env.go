package config

import (
	"os"
	"regexp"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// expandEnvVars replaces ${VAR} patterns with environment variable values.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name from ${VAR}.
		varName := envVarPattern.FindStringSubmatch(match)[1]

		value, exists := os.LookupEnv(varName)
		if exists {
			return value
		}

		// Return original if env var not found.
		return match
	})
}

// expandAliasEnvVars expands environment variables in all alias fields.
func expandAliasEnvVars(alias *Alias) {
	alias.Endpoint = expandEnvVars(alias.Endpoint)
	alias.Region = expandEnvVars(alias.Region)
	alias.Bucket = expandEnvVars(alias.Bucket)
	alias.Prefix = expandEnvVars(alias.Prefix)
	alias.AccessKey = expandEnvVars(alias.AccessKey)
	alias.SecretKey = expandEnvVars(alias.SecretKey)
}
