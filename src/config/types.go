package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the root configuration structure.
type Config struct {
	Aliases  map[string]Alias `yaml:"aliases"`
	Cache    CacheConfig      `yaml:"cache"`
	Settings Settings         `yaml:"settings"`
	Files    []FileEntry      `yaml:"files"`
}

// Alias represents an S3 storage backend configuration.
type Alias struct {
	Endpoint      string `yaml:"endpoint"`
	Region        string `yaml:"region"`
	Bucket        string `yaml:"bucket"`
	Prefix        string `yaml:"prefix"`
	AccessKey     string `yaml:"access_key"`
	SecretKey     string `yaml:"secret_key"`
	NoSignRequest string `yaml:"no_sign_request"`
}

// IsNoSignRequest returns true if no_sign_request is enabled.
// Accepts "true", "1", "yes" (case-insensitive) as truthy values.
func (alias Alias) IsNoSignRequest() bool {
	v := strings.ToLower(strings.TrimSpace(alias.NoSignRequest))

	return v == "true" || v == "1" || v == "yes"
}

// CacheConfig represents the cache configuration.
type CacheConfig struct {
	Alias   string `yaml:"alias"`
	Enabled string `yaml:"enabled"`
}

// IsEnabled returns true if cache is enabled.
// Accepts "true", "1", "yes" (case-insensitive) as truthy values.
func (c CacheConfig) IsEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(c.Enabled))

	return v == "true" || v == "1" || v == "yes"
}

// Settings represents download settings.
type Settings struct {
	Parallel        int           `yaml:"parallel"`
	Retries         int           `yaml:"retries"`
	RetryDelay      time.Duration `yaml:"retry_delay"`
	Timeout         time.Duration `yaml:"timeout"`
	SegmentsPerFile int           `yaml:"segments_per_file"`
	SegmentMinSize  int64         `yaml:"segment_min_size"`
	SingleStream    string        `yaml:"single_stream"`
}

// IsSingleStream returns true if segmented download is disabled.
// Accepts "true", "1", "yes" (case-insensitive) as truthy values.
func (settings Settings) IsSingleStream() bool {
	v := strings.ToLower(strings.TrimSpace(settings.SingleStream))

	return v == "true" || v == "1" || v == "yes"
}

// UnmarshalYAML expands ${VAR} env vars in each setting before parsing it into
// the typed field. Empty values are left as the zero value so defaults apply.
func (settings *Settings) UnmarshalYAML(value *yaml.Node) error {
	// raw mirrors Settings with all fields as strings so each value can be
	// env-expanded before parsing into the typed field. Keep in sync with Settings.
	var raw struct {
		Parallel        string `yaml:"parallel"`
		Retries         string `yaml:"retries"`
		RetryDelay      string `yaml:"retry_delay"`
		Timeout         string `yaml:"timeout"`
		SegmentsPerFile string `yaml:"segments_per_file"`
		SegmentMinSize  string `yaml:"segment_min_size"`
		SingleStream    string `yaml:"single_stream"`
	}

	err := value.Decode(&raw)
	if err != nil {
		return fmt.Errorf("decoding settings: %w", err)
	}

	err = parseIntSetting("parallel", raw.Parallel, &settings.Parallel)
	if err != nil {
		return err
	}

	err = parseIntSetting("retries", raw.Retries, &settings.Retries)
	if err != nil {
		return err
	}

	err = parseIntSetting("segments_per_file", raw.SegmentsPerFile, &settings.SegmentsPerFile)
	if err != nil {
		return err
	}

	err = parseInt64Setting("segment_min_size", raw.SegmentMinSize, &settings.SegmentMinSize)
	if err != nil {
		return err
	}

	err = parseDurationSetting("retry_delay", raw.RetryDelay, &settings.RetryDelay)
	if err != nil {
		return err
	}

	err = parseDurationSetting("timeout", raw.Timeout, &settings.Timeout)
	if err != nil {
		return err
	}

	settings.SingleStream = strings.TrimSpace(expandEnvVars(raw.SingleStream))

	return nil
}

// parseIntSetting expands env vars in raw and parses it into target.
// An empty expanded value leaves target untouched so defaults can apply.
func parseIntSetting(name, raw string, target *int) error {
	value := strings.TrimSpace(expandEnvVars(raw))
	if value == "" {
		return nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("parsing settings.%s %q: %w", name, value, err)
	}

	*target = parsed

	return nil
}

// parseInt64Setting expands env vars in raw and parses it into target.
func parseInt64Setting(name, raw string, target *int64) error {
	value := strings.TrimSpace(expandEnvVars(raw))
	if value == "" {
		return nil
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing settings.%s %q: %w", name, value, err)
	}

	*target = parsed

	return nil
}

// parseDurationSetting expands env vars in raw and parses it into target.
func parseDurationSetting(name, raw string, target *time.Duration) error {
	value := strings.TrimSpace(expandEnvVars(raw))
	if value == "" {
		return nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parsing settings.%s %q: %w", name, value, err)
	}

	*target = parsed

	return nil
}

// FileEntry represents a file to download.
type FileEntry struct {
	URL    string `yaml:"url"`
	Dest   string `yaml:"dest"`
	SHA256 string `yaml:"sha256"`
}
