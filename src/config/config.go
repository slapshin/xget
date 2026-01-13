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

// LoadMultiple reads and merges multiple YAML config files.
// Later configs override earlier ones for aliases, cache, and settings.
// Files are accumulated across all configs.
func LoadMultiple(paths []string) (*Config, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no config files specified")
	}

	// Load first config without validation.
	baseConfig, err := loadWithoutValidation(paths[0])
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", paths[0], err)
	}

	// Merge remaining configs.
	for _, path := range paths[1:] {
		cfg, err := loadWithoutValidation(path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}

		mergeConfigs(baseConfig, cfg)
	}

	// Apply defaults.
	applyDefaults(baseConfig)

	// Validate merged configuration.
	err = validate(baseConfig)
	if err != nil {
		return nil, fmt.Errorf("validating merged config: %w", err)
	}

	return baseConfig, nil
}

func loadWithoutValidation(path string) (*Config, error) {
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

	return &cfg, nil
}

func mergeConfigs(base *Config, override *Config) {
	// Merge aliases (add new or override existing).
	if base.Aliases == nil {
		base.Aliases = make(map[string]Alias)
	}

	for name, alias := range override.Aliases {
		base.Aliases[name] = alias
	}

	// Override cache settings if specified.
	if override.Cache.Alias != "" {
		base.Cache.Alias = override.Cache.Alias
	}

	if override.Cache.Enabled {
		base.Cache.Enabled = true
	}

	// Override settings if specified (non-zero values).
	if override.Settings.Parallel > 0 {
		base.Settings.Parallel = override.Settings.Parallel
	}

	if override.Settings.Retries > 0 {
		base.Settings.Retries = override.Settings.Retries
	}

	if override.Settings.RetryDelay > 0 {
		base.Settings.RetryDelay = override.Settings.RetryDelay
	}

	// Accumulate files.
	base.Files = append(base.Files, override.Files...)
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
