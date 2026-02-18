package config

import "time"

// Config represents the root configuration structure.
type Config struct {
	Aliases  map[string]Alias `yaml:"aliases"`
	Cache    CacheConfig      `yaml:"cache"`
	Settings Settings         `yaml:"settings"`
	Files    []FileEntry      `yaml:"files"`
}

// Alias represents an S3 storage backend configuration.
type Alias struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	Bucket    string `yaml:"bucket"`
	Prefix    string `yaml:"prefix"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

// CacheConfig represents the cache configuration.
type CacheConfig struct {
	Alias   string `yaml:"alias"`
	Enabled bool   `yaml:"enabled"`
}

// Settings represents download settings.
type Settings struct {
	Parallel   int           `yaml:"parallel"`
	Retries    int           `yaml:"retries"`
	RetryDelay time.Duration `yaml:"retry_delay"`
	Timeout    time.Duration `yaml:"timeout"`
}

// FileEntry represents a file to download.
type FileEntry struct {
	URL    string `yaml:"url"`
	Dest   string `yaml:"dest"`
	SHA256 string `yaml:"sha256"`
}
