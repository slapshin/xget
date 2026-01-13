package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseMultiple(t *testing.T) {
	tests := []struct {
		name      string
		configs   []string
		wantErr   bool
		errSubstr string
		validate  func(t *testing.T, cfg *Config)
	}{
		{
			name: "single config",
			configs: []string{`
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
`},
			validate: func(t *testing.T, cfg *Config) {
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

				if cfg.Files[0].URL != "http://example.com/file1.txt" {
					t.Errorf("expected URL 'http://example.com/file1.txt', got %s", cfg.Files[0].URL)
				}

				if cfg.Files[0].Dest != "/tmp/file1.txt" {
					t.Errorf("expected dest '/tmp/file1.txt', got %s", cfg.Files[0].Dest)
				}

				if cfg.Files[0].SHA256 != "abc123" {
					t.Errorf("expected sha256 'abc123', got %s", cfg.Files[0].SHA256)
				}
			},
		},
		{
			name: "alias override and add",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
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
			},
		},
		{
			name: "cache complete override",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
				if !cfg.Cache.Enabled {
					t.Error("expected cache to be enabled")
				}

				if cfg.Cache.Alias != "new-cache" {
					t.Errorf("expected cache alias 'new-cache', got %s", cfg.Cache.Alias)
				}
			},
		},
		{
			name: "cache alias only override",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
				if !cfg.Cache.Enabled {
					t.Error("expected cache to be enabled")
				}

				if cfg.Cache.Alias != "new-cache" {
					t.Errorf("expected cache alias 'new-cache', got %s", cfg.Cache.Alias)
				}
			},
		},
		{
			name: "cache enable with alias",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
				if !cfg.Cache.Enabled {
					t.Error("expected cache to be enabled")
				}

				if cfg.Cache.Alias != "cache-alias" {
					t.Errorf("expected cache alias 'cache-alias', got %s", cfg.Cache.Alias)
				}
			},
		},
		{
			name: "settings partial override",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Settings.Parallel != 10 {
					t.Errorf("expected parallel 10, got %d", cfg.Settings.Parallel)
				}

				if cfg.Settings.Retries != 3 {
					t.Errorf("expected retries 3 (unchanged), got %d", cfg.Settings.Retries)
				}

				if cfg.Settings.RetryDelay != 20*time.Second {
					t.Errorf("expected retry_delay 20s, got %v", cfg.Settings.RetryDelay)
				}
			},
		},
		{
			name: "settings zero values don't override",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Settings.Parallel != 5 {
					t.Errorf("expected parallel 5 (unchanged), got %d", cfg.Settings.Parallel)
				}

				if cfg.Settings.Retries != 3 {
					t.Errorf("expected retries 3 (unchanged), got %d", cfg.Settings.Retries)
				}

				if cfg.Settings.RetryDelay != 10*time.Second {
					t.Errorf("expected retry_delay 10s (unchanged), got %v", cfg.Settings.RetryDelay)
				}
			},
		},
		{
			name: "files accumulate",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
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
			},
		},
		{
			name: "three configs merge",
			configs: []string{
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
			},
			validate: func(t *testing.T, cfg *Config) {
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

				alias2, exists := cfg.Aliases["alias2"]
				if !exists {
					t.Error("expected alias2 to exist")
				}

				if alias2.Endpoint != "http://endpoint2" {
					t.Errorf("expected endpoint 'http://endpoint2', got %s", alias2.Endpoint)
				}

				if cfg.Settings.Parallel != 10 {
					t.Errorf("expected parallel 10 (from config3), got %d", cfg.Settings.Parallel)
				}

				if cfg.Settings.Retries != 5 {
					t.Errorf("expected retries 5 (from config2), got %d", cfg.Settings.Retries)
				}

				if cfg.Settings.RetryDelay != 15*time.Second {
					t.Errorf("expected retry_delay 15s (from config3), got %v", cfg.Settings.RetryDelay)
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
			},
		},
		{
			name: "defaults applied",
			configs: []string{`
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
`},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Settings.Parallel != defaultParallel {
					t.Errorf("expected parallel %d, got %d", defaultParallel, cfg.Settings.Parallel)
				}

				if cfg.Settings.Retries != defaultRetries {
					t.Errorf("expected retries %d, got %d", defaultRetries, cfg.Settings.Retries)
				}

				if cfg.Settings.RetryDelay != defaultRetryDelay {
					t.Errorf("expected retry_delay %v, got %v", defaultRetryDelay, cfg.Settings.RetryDelay)
				}
			},
		},
		{
			name:      "empty config list",
			configs:   []string{},
			wantErr:   true,
			errSubstr: "no configs specified",
		},
		{
			name:      "invalid yaml",
			configs:   []string{"invalid: yaml: content: ["},
			wantErr:   true,
			errSubstr: "parsing config",
		},
		{
			name: "validation error - cache enabled without alias",
			configs: []string{`
cache:
  enabled: true

files:
  - url: http://example.com/file1.txt
    dest: /tmp/file1.txt
    sha256: abc123
`},
			wantErr:   true,
			errSubstr: "validating merged config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configBytes := make([][]byte, len(tt.configs))
			for i, c := range tt.configs {
				configBytes[i] = []byte(c)
			}

			cfg, err := ParseMultiple(configBytes)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got: %v", tt.errSubstr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
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
