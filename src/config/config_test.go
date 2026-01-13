package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMultiple_SingleConfig(t *testing.T) {
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
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
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadMultiple([]string{configPath})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config to be non-nil")
	}

	// Verify aliases.
	if len(cfg.Aliases) != 1 {
		t.Errorf("expected 1 alias, got %d", len(cfg.Aliases))
	}

	alias, exists := cfg.Aliases["test-alias"]
	if !exists {
		t.Error("expected alias 'test-alias' to exist")
	}

	if alias.Endpoint != "http://localhost:9000" {
		t.Errorf("expected endpoint 'http://localhost:9000', got %s", alias.Endpoint)
	}

	if alias.Region != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got %s", alias.Region)
	}

	if alias.Bucket != "test-bucket" {
		t.Errorf("expected bucket 'test-bucket', got %s", alias.Bucket)
	}

	// Verify settings.
	if cfg.Settings.Parallel != 5 {
		t.Errorf("expected parallel 5, got %d", cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != 3 {
		t.Errorf("expected retries 3, got %d", cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != 10*time.Second {
		t.Errorf("expected retry_delay 10s, got %v", cfg.Settings.RetryDelay)
	}

	// Verify files.
	if len(cfg.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(cfg.Files))
	}

	if cfg.Files[0].URL != "http://example.com/file1.txt" {
		t.Errorf("expected URL 'http://example.com/file1.txt', got %s", cfg.Files[0].URL)
	}

	if cfg.Files[0].Dest != "/tmp/file1.txt" {
		t.Errorf("expected dest '/tmp/file1.txt', got %s", cfg.Files[0].Dest)
	}

	if cfg.Files[0].SHA256 != "abc123" {
		t.Errorf("expected sha256 'abc123', got %s", cfg.Files[0].SHA256)
	}
}

func TestLoadMultiple_AliasOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config with two aliases.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config that overrides alias1 and adds alias3.
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
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
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify alias1 was overridden.
	alias1, exists := cfg.Aliases["alias1"]
	if !exists {
		t.Error("expected alias1 to exist")
	}

	if alias1.Endpoint != "http://new-endpoint1" {
		t.Errorf("expected endpoint 'http://new-endpoint1', got %s", alias1.Endpoint)
	}

	if alias1.Region != "eu-west-1" {
		t.Errorf("expected region 'eu-west-1', got %s", alias1.Region)
	}

	if alias1.Bucket != "new-bucket1" {
		t.Errorf("expected bucket 'new-bucket1', got %s", alias1.Bucket)
	}

	// Verify alias2 remains unchanged.
	alias2, exists := cfg.Aliases["alias2"]
	if !exists {
		t.Error("expected alias2 to exist")
	}

	if alias2.Endpoint != "http://old-endpoint2" {
		t.Errorf("expected endpoint 'http://old-endpoint2', got %s", alias2.Endpoint)
	}

	if alias2.Region != "us-east-2" {
		t.Errorf("expected region 'us-east-2', got %s", alias2.Region)
	}

	// Verify alias3 was added.
	alias3, exists := cfg.Aliases["alias3"]
	if !exists {
		t.Error("expected alias3 to exist")
	}

	if alias3.Endpoint != "http://endpoint3" {
		t.Errorf("expected endpoint 'http://endpoint3', got %s", alias3.Endpoint)
	}

	if alias3.Region != "ap-south-1" {
		t.Errorf("expected region 'ap-south-1', got %s", alias3.Region)
	}
}

func TestLoadMultiple_CacheOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config with cache enabled.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config with different cache alias.
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
cache:
  alias: new-cache
  enabled: true
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify cache was overridden.
	if !cfg.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if cfg.Cache.Alias != "new-cache" {
		t.Errorf("expected cache alias 'new-cache', got %s", cfg.Cache.Alias)
	}
}

func TestLoadMultiple_CacheAliasOnly(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config with cache enabled.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config with only alias (no enabled field).
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
cache:
  alias: new-cache
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify cache alias was overridden but enabled stays true from base.
	if !cfg.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if cfg.Cache.Alias != "new-cache" {
		t.Errorf("expected cache alias 'new-cache', got %s", cfg.Cache.Alias)
	}
}

func TestLoadMultiple_CacheEnableOnly(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config without cache.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config that enables cache with alias.
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
cache:
  alias: cache-alias
  enabled: true
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify cache was enabled.
	if !cfg.Cache.Enabled {
		t.Error("expected cache to be enabled")
	}

	if cfg.Cache.Alias != "cache-alias" {
		t.Errorf("expected cache alias 'cache-alias', got %s", cfg.Cache.Alias)
	}
}

func TestLoadMultiple_SettingsOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config with settings.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config with partial settings override.
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
settings:
  parallel: 10
  retry_delay: 20s
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify partial override.
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

func TestLoadMultiple_SettingsZeroValue(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config with settings.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config with no settings (zero values).
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
settings:
  parallel: 0
  retries: 0
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify zero values don't override.
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

func TestLoadMultiple_FilesAccumulate(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config with one file.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Override config with additional files.
	overrideConfig := filepath.Join(tmpDir, "override.yaml")
	overrideContent := `
files:
  - url: http://example.com/file2.txt
    dest: /tmp/file2.txt
    sha256: def456
  - url: http://example.com/file3.txt
    dest: /tmp/file3.txt
    sha256: ghi789
`
	err = os.WriteFile(overrideConfig, []byte(overrideContent), 0644)
	if err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, overrideConfig})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify files were accumulated.
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

func TestLoadMultiple_ThreeConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	// Base config.
	baseConfig := filepath.Join(tmpDir, "base.yaml")
	baseContent := `
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
`
	err := os.WriteFile(baseConfig, []byte(baseContent), 0644)
	if err != nil {
		t.Fatalf("failed to write base config: %v", err)
	}

	// Second config.
	config2 := filepath.Join(tmpDir, "config2.yaml")
	config2Content := `
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
`
	err = os.WriteFile(config2, []byte(config2Content), 0644)
	if err != nil {
		t.Fatalf("failed to write config2: %v", err)
	}

	// Third config.
	config3 := filepath.Join(tmpDir, "config3.yaml")
	config3Content := `
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
`
	err = os.WriteFile(config3, []byte(config3Content), 0644)
	if err != nil {
		t.Fatalf("failed to write config3: %v", err)
	}

	cfg, err := LoadMultiple([]string{baseConfig, config2, config3})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify alias1 was overridden by config3.
	alias1, exists := cfg.Aliases["alias1"]
	if !exists {
		t.Error("expected alias1 to exist")
	}

	if alias1.Endpoint != "http://new-endpoint1" {
		t.Errorf("expected endpoint 'http://new-endpoint1', got %s", alias1.Endpoint)
	}

	if alias1.Region != "ap-south-1" {
		t.Errorf("expected region 'ap-south-1', got %s", alias1.Region)
	}

	// Verify alias2 from config2 exists.
	alias2, exists := cfg.Aliases["alias2"]
	if !exists {
		t.Error("expected alias2 to exist")
	}

	if alias2.Endpoint != "http://endpoint2" {
		t.Errorf("expected endpoint 'http://endpoint2', got %s", alias2.Endpoint)
	}

	// Verify settings from all configs.
	if cfg.Settings.Parallel != 10 {
		t.Errorf("expected parallel 10 (from config3), got %d", cfg.Settings.Parallel)
	}

	if cfg.Settings.Retries != 5 {
		t.Errorf("expected retries 5 (from config2), got %d", cfg.Settings.Retries)
	}

	if cfg.Settings.RetryDelay != 15*time.Second {
		t.Errorf("expected retry_delay 15s (from config3), got %v", cfg.Settings.RetryDelay)
	}

	// Verify all files accumulated.
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

func TestLoadMultiple_EmptyPathList(t *testing.T) {
	_, err := LoadMultiple([]string{})
	if err == nil {
		t.Error("expected error for empty path list")
	}

	if !strings.Contains(err.Error(), "no config files specified") {
		t.Errorf("expected error message to contain 'no config files specified', got: %v", err)
	}
}

func TestLoadMultiple_NonExistentFile(t *testing.T) {
	_, err := LoadMultiple([]string{"/nonexistent/config.yaml"})
	if err == nil {
		t.Error("expected error for non-existent file")
	}

	if !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("expected error message to contain 'reading config file', got: %v", err)
	}
}

func TestLoadMultiple_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644)
	if err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	_, err = LoadMultiple([]string{configPath})
	if err == nil {
		t.Error("expected error for invalid YAML")
	}

	if !strings.Contains(err.Error(), "parsing config file") {
		t.Errorf("expected error message to contain 'parsing config file', got: %v", err)
	}
}

func TestLoadMultiple_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()

	// Config with cache enabled but no alias.
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
cache:
  enabled: true

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err = LoadMultiple([]string{configPath})
	if err == nil {
		t.Error("expected validation error")
	}

	if !strings.Contains(err.Error(), "validating merged config") {
		t.Errorf("expected error message to contain 'validating merged config', got: %v", err)
	}
}

func TestLoadMultiple_Defaults(t *testing.T) {
	tmpDir := t.TempDir()

	// Config with no settings specified.
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
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
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadMultiple([]string{configPath})
	if err != nil {
		t.Fatalf("LoadMultiple failed: %v", err)
	}

	// Verify defaults were applied.
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

	// Verify base config unchanged.
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
