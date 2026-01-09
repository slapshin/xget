package config

import (
	"os"
	"regexp"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ExpandEnvVars replaces ${VAR} patterns with environment variable values.
func ExpandEnvVars(s string) string {
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
	alias.Endpoint = ExpandEnvVars(alias.Endpoint)
	alias.Region = ExpandEnvVars(alias.Region)
	alias.Bucket = ExpandEnvVars(alias.Bucket)
	alias.Prefix = ExpandEnvVars(alias.Prefix)
	alias.AccessKey = ExpandEnvVars(alias.AccessKey)
	alias.SecretKey = ExpandEnvVars(alias.SecretKey)
}
