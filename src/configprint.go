package main

import (
	"fmt"
	"regexp"
	"sort"

	"xget/src/config"
	"xget/src/redact"
)

// printConfig prints a human-readable summary of the effective config,
// masking credentials in aliases and userinfo in file URLs.
func printConfig(cfg config.Config) {
	fmt.Println("\nsettings:")
	fmt.Printf("  parallel:          %d\n", cfg.Settings.Parallel)
	fmt.Printf("  retries:           %d\n", cfg.Settings.Retries)
	fmt.Printf("  retry_delay:       %s\n", cfg.Settings.RetryDelay)
	fmt.Printf("  timeout:           %s\n", cfg.Settings.Timeout)
	fmt.Printf("  segments_per_file: %d\n", cfg.Settings.SegmentsPerFile)
	fmt.Printf("  segment_min_size:  %d\n", cfg.Settings.SegmentMinSize)
	fmt.Printf("  single_stream:     %t\n", cfg.Settings.IsSingleStream())
	fmt.Printf("  debug:             %t\n", cfg.Settings.IsDebug())
	fmt.Printf("  debug_interval:    %s\n", cfg.Settings.DebugInterval)
	fmt.Printf("  full_verify:       %t\n", cfg.Settings.IsFullVerify())

	fmt.Println("cache:")
	fmt.Printf("  enabled: %t\n", cfg.Cache.IsEnabled())
	fmt.Printf("  alias:   %s\n", cfg.Cache.Alias)

	printAliases(cfg.Aliases)
	printFiles(cfg.Files)

	fmt.Println()
}

// printAliases prints each alias with credentials masked.
func printAliases(aliases map[string]config.Alias) {
	fmt.Printf("aliases (%d):\n", len(aliases))

	for _, name := range sortedAliasNames(aliases) {
		alias := aliases[name]

		fmt.Printf("  %s:\n", name)
		fmt.Printf("    endpoint:        %s\n", alias.Endpoint)
		fmt.Printf("    region:          %s\n", alias.Region)
		fmt.Printf("    bucket:          %s\n", alias.Bucket)
		fmt.Printf("    prefix:          %s\n", alias.Prefix)
		fmt.Printf("    access_key:      %s\n", maskTail(alias.AccessKey))
		fmt.Printf("    secret_key:      %s\n", maskTail(alias.SecretKey))
		fmt.Printf("    no_sign_request: %t\n", alias.IsNoSignRequest())
	}
}

// printFiles prints each file entry with URL userinfo redacted.
func printFiles(files []config.FileEntry) {
	fmt.Printf("files (%d):\n", len(files))

	for _, file := range files {
		fmt.Printf("  - url:  %s\n", redact.URL(file.URL))
		fmt.Printf("    dest: %s\n", file.Dest)

		if file.SHA256 != "" {
			fmt.Printf("    sha256: %s\n", file.SHA256)
		}
	}
}

// sortedAliasNames returns the alias names sorted for stable output.
func sortedAliasNames(aliases map[string]config.Alias) []string {
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// envPlaceholderPattern matches an unexpanded ${VAR} env var reference.
var envPlaceholderPattern = regexp.MustCompile(`\$\{[^}]+\}`)

// maskTail masks a credential, reporting unset values explicitly.
// A value still holding an unexpanded ${VAR} placeholder is not a real secret,
// so it is shown verbatim to surface the missing environment variable.
func maskTail(secret string) string {
	if secret == "" {
		return "<not set>"
	}

	if envPlaceholderPattern.MatchString(secret) {
		return secret
	}

	return redact.Secret(secret)
}
