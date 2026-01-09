package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultParallel   = 4
	defaultRetries    = 3
	defaultRetryDelay = 5 * time.Second
)

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Expand environment variables in aliases.
	for name, alias := range cfg.Aliases {
		expandAliasEnvVars(&alias)
		cfg.Aliases[name] = alias
	}

	// Apply defaults.
	applyDefaults(&cfg)

	// Validate configuration.
	err = validate(&cfg)
	if err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Settings.Parallel <= 0 {
		cfg.Settings.Parallel = defaultParallel
	}

	if cfg.Settings.Retries <= 0 {
		cfg.Settings.Retries = defaultRetries
	}

	if cfg.Settings.RetryDelay <= 0 {
		cfg.Settings.RetryDelay = defaultRetryDelay
	}
}

func validate(cfg *Config) error {
	// Validate cache alias exists if cache is enabled.
	if cfg.Cache.Enabled {
		if cfg.Cache.Alias == "" {
			return fmt.Errorf("cache enabled but no alias specified")
		}

		if _, exists := cfg.Aliases[cfg.Cache.Alias]; !exists {
			return fmt.Errorf("cache alias %q not found in aliases", cfg.Cache.Alias)
		}
	}

	// Validate files.
	for i, file := range cfg.Files {
		if file.URL == "" {
			return fmt.Errorf("file %d: url is required", i)
		}

		if file.Dest == "" {
			return fmt.Errorf("file %d: dest is required", i)
		}

		if file.SHA256 == "" {
			return fmt.Errorf("file %d: sha256 is required", i)
		}
	}

	return nil
}

// GetAlias returns an alias by name.
func (config *Config) GetAlias(name string) (Alias, bool) {
	alias, exists := config.Aliases[name]

	return alias, exists
}

// GetCacheAlias returns the cache alias if cache is enabled.
func (config *Config) GetCacheAlias() (Alias, bool) {
	if !config.Cache.Enabled {
		return Alias{}, false
	}

	return config.GetAlias(config.Cache.Alias)
}
