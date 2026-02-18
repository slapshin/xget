package config

import (
	"strings"
	"testing"
	"time"
)

// helper function to parse configs and handle errors.
func parseConfigs(t *testing.T, configs []string) (*Config, error) {
	t.Helper()

	configBytes := make([][]byte, len(configs))
	for i, c := range configs {
		configBytes[i] = []byte(c)
	}

	return ParseMultiple(configBytes)
}

// assertAliasFields validates all fields of an alias.
func assertAliasFields(t *testing.T, alias Alias, endpoint, region, bucket string) {
	t.Helper()

	if alias.Endpoint != endpoint {
		t.Errorf("expected endpoint '%s', got %s", endpoint, alias.Endpoint)
	}

	if alias.Region != region {
		t.Errorf("expected region '%s', got %s", region, alias.Region)
	}

	if alias.Bucket != bucket {
		t.Errorf("expected bucket '%s', got %s", bucket, alias.Bucket)
	}
}

// assertFileEntry validates a file entry.
func assertFileEntry(t *testing.T, file FileEntry, url, dest, sha256 string) {
	t.Helper()

	if file.URL != url {
		t.Errorf("expected URL '%s', got %s", url, file.URL)
	}

	if file.Dest != dest {
		t.Errorf("expected dest '%s', got %s", dest, file.Dest)
	}

	if file.SHA256 != sha256 {
		t.Errorf("expected sha256 '%s', got %s", sha256, file.SHA256)
	}
}

func TestParseMultiple_SingleConfig(t *testing.T) {
	cfg, err := parseConfigs(t, []string{`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: test-key
    secret_key: test-secret

settings:
  parallel: 5
  retries: 3
  retry_delay: 10s

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Aliases) != 1 {
		t.Errorf("expected 1 alias, got %d", len(cfg.Aliases))
	}

	alias, exists := cfg.Aliases["test-alias"]
	if !exists {
		t.Error("expected alias 'test-alias' to exist")
	}

	assertAliasFields(t, alias, "http://localhost:9000", "us-east-1", "test-bucket")

	if cfg.Settings.Parallel != 5 {
		t.Errorf("expected parallel 5, got %d", cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != 3 {
		t.Errorf("expected retries 3, got %d", cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != 10*time.Second {
		t.Errorf("expected retry_delay 10s, got %v", cfg.Settings.RetryDelay)
	}

	if len(cfg.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(cfg.Files))
	}

	assertFileEntry(t, cfg.Files[0], "http://example.com/file1.txt", "/tmp/file1.txt", "abc123")
}

func TestParseMultiple_AliasOverrideAndAdd(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  alias1:
    endpoint: http://old-endpoint1
    region: us-east-1
    bucket: old-bucket1
    access_key: old-key1
    secret_key: old-secret1
  alias2:
    endpoint: http://old-endpoint2
    region: us-east-2
    bucket: old-bucket2
    access_key: old-key2
    secret_key: old-secret2

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
aliases:
  alias1:
    endpoint: http://new-endpoint1
    region: eu-west-1
    bucket: new-bucket1
    access_key: new-key1
    secret_key: new-secret1
  alias3:
    endpoint: http://endpoint3
    region: ap-south-1
    bucket: bucket3
    access_key: key3
    secret_key: secret3
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// alias1 should be overridden
	alias1, exists := cfg.Aliases["alias1"]
	if !exists {
		t.Error("expected alias1 to exist")
	}

	assertAliasFields(t, alias1, "http://new-endpoint1", "eu-west-1", "new-bucket1")

	// alias2 should remain unchanged
	alias2, exists := cfg.Aliases["alias2"]
	if !exists {
		t.Error("expected alias2 to exist")
	}

	assertAliasFields(t, alias2, "http://old-endpoint2", "us-east-2", "old-bucket2")

	// alias3 should be added
	alias3, exists := cfg.Aliases["alias3"]
	if !exists {
		t.Error("expected alias3 to exist")
	}

	assertAliasFields(t, alias3, "http://endpoint3", "ap-south-1", "bucket3")
}

func TestParseMultiple_CacheCompleteOverride(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  cache-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: cache-bucket
    access_key: key
    secret_key: secret
  new-cache:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: new-cache-bucket
    access_key: key
    secret_key: secret

cache:
  alias: cache-alias
  enabled: true

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
cache:
  alias: new-cache
  enabled: true
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if cfg.Cache.Alias != "new-cache" {
		t.Errorf("expected cache alias 'new-cache', got %s", cfg.Cache.Alias)
	}
}

func TestParseMultiple_CacheAliasOnlyOverride(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  cache-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: cache-bucket
    access_key: key
    secret_key: secret
  new-cache:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: new-cache-bucket
    access_key: key
    secret_key: secret

cache:
  alias: cache-alias
  enabled: true

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
cache:
  alias: new-cache
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if cfg.Cache.Alias != "new-cache" {
		t.Errorf("expected cache alias 'new-cache', got %s", cfg.Cache.Alias)
	}
}

func TestParseMultiple_CacheEnableWithAlias(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  cache-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: cache-bucket
    access_key: key
    secret_key: secret

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
cache:
  alias: cache-alias
  enabled: true
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if cfg.Cache.Alias != "cache-alias" {
		t.Errorf("expected cache alias 'cache-alias', got %s", cfg.Cache.Alias)
	}
}

func TestParseMultiple_SettingsPartialOverride(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret

settings:
  parallel: 5
  retries: 3
  retry_delay: 10s

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
settings:
  parallel: 10
  retry_delay: 20s
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Settings.Parallel != 10 {
		t.Errorf("expected parallel 10, got %d", cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != 3 {
		t.Errorf("expected retries 3 (unchanged), got %d", cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != 20*time.Second {
		t.Errorf("expected retry_delay 20s, got %v", cfg.Settings.RetryDelay)
	}
}

func TestParseMultiple_SettingsZeroValuesDontOverride(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret

settings:
  parallel: 5
  retries: 3
  retry_delay: 10s

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
settings:
  parallel: 0
  retries: 0
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Settings.Parallel != 5 {
		t.Errorf("expected parallel 5 (unchanged), got %d", cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != 3 {
		t.Errorf("expected retries 3 (unchanged), got %d", cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != 10*time.Second {
		t.Errorf("expected retry_delay 10s (unchanged), got %v", cfg.Settings.RetryDelay)
	}
}

func TestParseMultiple_FilesAccumulate(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
files:
  - url: http://example.com/file2.txt
    dest: /tmp/file2.txt
    sha256: def456
  - url: http://example.com/file3.txt
    dest: /tmp/file3.txt
    sha256: ghi789
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(cfg.Files))
	}

	if cfg.Files[0].URL != "http://example.com/file1.txt" {
		t.Errorf("expected first URL 'http://example.com/file1.txt', got %s", cfg.Files[0].URL)
	}

	if cfg.Files[1].URL != "http://example.com/file2.txt" {
		t.Errorf("expected second URL 'http://example.com/file2.txt', got %s", cfg.Files[1].URL)
	}

	if cfg.Files[2].URL != "http://example.com/file3.txt" {
		t.Errorf("expected third URL 'http://example.com/file3.txt', got %s", cfg.Files[2].URL)
	}
}

func TestParseMultiple_ThreeConfigsMerge(t *testing.T) {
	cfg, err := parseConfigs(t, []string{
		`
aliases:
  alias1:
    endpoint: http://endpoint1
    region: us-east-1
    bucket: bucket1
    access_key: key1
    secret_key: secret1

settings:
  parallel: 5

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`,
		`
aliases:
  alias2:
    endpoint: http://endpoint2
    region: eu-west-1
    bucket: bucket2
    access_key: key2
    secret_key: secret2

settings:
  retries: 5

files:
  - url: http://example.com/file2.txt
    dest: /tmp/file2.txt
    sha256: def456
`,
		`
aliases:
  alias1:
    endpoint: http://new-endpoint1
    region: ap-south-1
    bucket: new-bucket1
    access_key: new-key1
    secret_key: new-secret1

settings:
  parallel: 10
  retry_delay: 15s

files:
  - url: http://example.com/file3.txt
    dest: /tmp/file3.txt
    sha256: ghi789
`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// alias1 should be overridden by config3
	alias1, exists := cfg.Aliases["alias1"]
	if !exists {
		t.Error("expected alias1 to exist")
	}

	assertAliasFields(t, alias1, "http://new-endpoint1", "ap-south-1", "new-bucket1")

	// alias2 should exist from config2
	alias2, exists := cfg.Aliases["alias2"]
	if !exists {
		t.Error("expected alias2 to exist")
	}

	if alias2.Endpoint != "http://endpoint2" {
		t.Errorf("expected endpoint 'http://endpoint2', got %s", alias2.Endpoint)
	}

	// settings should be merged from all configs
	if cfg.Settings.Parallel != 10 {
		t.Errorf("expected parallel 10 (from config3), got %d", cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != 5 {
		t.Errorf("expected retries 5 (from config2), got %d", cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != 15*time.Second {
		t.Errorf("expected retry_delay 15s (from config3), got %v", cfg.Settings.RetryDelay)
	}

	// files should accumulate from all configs
	if len(cfg.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(cfg.Files))
	}

	assertFileEntry(t, cfg.Files[0], "http://example.com/file1.txt", "/tmp/file1.txt", "abc123")
	assertFileEntry(t, cfg.Files[1], "http://example.com/file2.txt", "/tmp/file2.txt", "def456")
	assertFileEntry(t, cfg.Files[2], "http://example.com/file3.txt", "/tmp/file3.txt", "ghi789")
}

func TestParseMultiple_DefaultsApplied(t *testing.T) {
	cfg, err := parseConfigs(t, []string{`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Settings.Parallel != defaultParallel {
		t.Errorf("expected parallel %d, got %d", defaultParallel, cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != defaultRetries {
		t.Errorf("expected retries %d, got %d", defaultRetries, cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != defaultRetryDelay {
		t.Errorf("expected retry_delay %v, got %v", defaultRetryDelay, cfg.Settings.RetryDelay)
	}
}

func TestParseMultiple_EmptyConfigList(t *testing.T) {
	_, err := parseConfigs(t, []string{})
	if err == nil {
		t.Error("expected error but got none")
	}

	if !strings.Contains(err.Error(), "no configs specified") {
		t.Errorf("expected error containing 'no configs specified', got: %v", err)
	}
}

func TestParseMultiple_InvalidYAML(t *testing.T) {
	_, err := parseConfigs(t, []string{"invalid: yaml: content: ["})
	if err == nil {
		t.Error("expected error but got none")
	}

	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("expected error containing 'parsing config', got: %v", err)
	}
}

func TestParseMultiple_ValidationError(t *testing.T) {
	_, err := parseConfigs(t, []string{`
cache:
  enabled: true

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`})
	if err == nil {
		t.Error("expected error but got none")
	}

	if !strings.Contains(err.Error(), "validating merged config") {
		t.Errorf("expected error containing 'validating merged config', got: %v", err)
	}
}

func TestMergeConfigs_NilAliases(t *testing.T) {
	base := &Config{}
	override := &Config{
		Aliases: map[string]Alias{
			"alias1": {
				Endpoint: "http://endpoint1",
				Region:   "us-east-1",
			},
		},
	}

	mergeConfigs(base, override)

	if len(base.Aliases) != 1 {
		t.Errorf("expected 1 alias, got %d", len(base.Aliases))
	}

	alias, exists := base.Aliases["alias1"]
	if !exists {
		t.Error("expected alias1 to exist")
	}

	if alias.Endpoint != "http://endpoint1" {
		t.Errorf("expected endpoint 'http://endpoint1', got %s", alias.Endpoint)
	}
}

func TestMergeConfigs_EmptyOverride(t *testing.T) {
	base := &Config{
		Aliases: map[string]Alias{
			"alias1": {
				Endpoint: "http://endpoint1",
				Region:   "us-east-1",
			},
		},
		Cache: CacheConfig{
			Alias:   "cache-alias",
			Enabled: true,
		},
		Settings: Settings{
			Parallel: 5,
			Retries:  3,
		},
		Files: []FileEntry{
			{URL: "http://example.com/file1.txt", Dest: "/tmp/file1.txt", SHA256: "abc123"},
		},
	}

	override := &Config{}

	mergeConfigs(base, override)

	if len(base.Aliases) != 1 {
		t.Errorf("expected 1 alias, got %d", len(base.Aliases))
	}

	if !base.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if base.Cache.Alias != "cache-alias" {
		t.Errorf("expected cache alias 'cache-alias', got %s", base.Cache.Alias)
	}

	if base.Settings.Parallel != 5 {
		t.Errorf("expected parallel 5, got %d", base.Settings.Parallel)
	}

	if base.Settings.Retries != 3 {
		t.Errorf("expected retries 3, got %d", base.Settings.Retries)
	}

	if len(base.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(base.Files))
	}
}

func TestFileEntryEnvVarExpansion(t *testing.T) {
	t.Setenv("DOWNLOAD_DIR", "/custom/downloads")
	t.Setenv("FILE_PREFIX", "output")

	cfg, err := parseConfigs(t, []string{`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret

files:
  - url: http://example.com/file1.txt
    dest: ${DOWNLOAD_DIR}/file1.txt
    sha256: abc123
  - url: http://example.com/file2.txt
    dest: ${DOWNLOAD_DIR}/${FILE_PREFIX}_file2.txt
    sha256: def456
  - url: http://example.com/file3.txt
    dest: /tmp/file3.txt
    sha256: ghi789
`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(cfg.Files))
	}

	assertFileEntry(t, cfg.Files[0], "http://example.com/file1.txt", "/custom/downloads/file1.txt", "abc123")
	assertFileEntry(t, cfg.Files[1], "http://example.com/file2.txt", "/custom/downloads/output_file2.txt", "def456")
	assertFileEntry(t, cfg.Files[2], "http://example.com/file3.txt", "/tmp/file3.txt", "ghi789")
}

func TestAliasNoSignRequestEnvVarExpansion(t *testing.T) {
	t.Setenv("NO_SIGN", "true")

	cfg, err := parseConfigs(t, []string{`
aliases:
  signed:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret
    no_sign_request: "false"
  unsigned:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    no_sign_request: ${NO_SIGN}

files:
  - url: s3://unsigned/file.txt
    dest: /tmp/file.txt
    sha256: abc123
`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	signed, ok := cfg.Aliases["signed"]
	if !ok {
		t.Fatal("expected 'signed' alias")
	}

	if signed.IsNoSignRequest() {
		t.Error("expected signed alias to have no_sign_request=false")
	}

	unsigned, ok := cfg.Aliases["unsigned"]
	if !ok {
		t.Fatal("expected 'unsigned' alias")
	}

	if !unsigned.IsNoSignRequest() {
		t.Error("expected unsigned alias to have no_sign_request=true via env var")
	}
}

func TestAliasNoSignRequestLiteralValues(t *testing.T) {
	cfg, err := parseConfigs(t, []string{`
aliases:
  by-true:
    endpoint: http://localhost:9000
    bucket: b
    no_sign_request: "true"
  by-one:
    endpoint: http://localhost:9000
    bucket: b
    no_sign_request: "1"
  by-yes:
    endpoint: http://localhost:9000
    bucket: b
    no_sign_request: "yes"
  by-false:
    endpoint: http://localhost:9000
    bucket: b
    no_sign_request: "false"
  by-empty:
    endpoint: http://localhost:9000
    bucket: b

files:
  - url: s3://by-true/file.txt
    dest: /tmp/file.txt
    sha256: abc123
`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		name     string
		expected bool
	}{
		{"by-true", true},
		{"by-one", true},
		{"by-yes", true},
		{"by-false", false},
		{"by-empty", false},
	}

	for _, tc := range cases {
		alias, ok := cfg.Aliases[tc.name]
		if !ok {
			t.Fatalf("expected alias %q", tc.name)
		}

		if alias.IsNoSignRequest() != tc.expected {
			t.Errorf("alias %q: expected IsNoSignRequest()=%v, got %v", tc.name, tc.expected, alias.IsNoSignRequest())
		}
	}
}

func TestFileEntryEnvVarNotExpanded(t *testing.T) {
	cfg, err := parseConfigs(t, []string{`
aliases:
  test-alias:
    endpoint: http://localhost:9000
    region: us-east-1
    bucket: test-bucket
    access_key: key
    secret_key: secret

files:
  - url: http://example.com/file1.txt
    dest: ${NONEXISTENT_VAR}/file1.txt
    sha256: abc123
`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(cfg.Files))
	}

	assertFileEntry(t, cfg.Files[0], "http://example.com/file1.txt", "${NONEXISTENT_VAR}/file1.txt", "abc123")
}
