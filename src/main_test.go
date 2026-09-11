package main

import (
	"testing"

	"xget/src/config"
)

func TestApplyDebugSettings(t *testing.T) {
	tests := []struct {
		name        string
		configValue string
		envValue    string
		envSet      bool
		debugFlag   bool
		wantDebug   bool
	}{
		{
			name:      "disabled by default",
			wantDebug: false,
		},
		{
			name:        "enabled by config",
			configValue: "true",
			wantDebug:   true,
		},
		{
			name:      "enabled by env var",
			envValue:  "1",
			envSet:    true,
			wantDebug: true,
		},
		{
			name:        "env var overrides config",
			configValue: "true",
			envValue:    "false",
			envSet:      true,
			wantDebug:   false,
		},
		{
			name:      "flag overrides env var",
			envValue:  "false",
			envSet:    true,
			debugFlag: true,
			wantDebug: true,
		},
		{
			name:        "flag overrides config",
			configValue: "false",
			debugFlag:   true,
			wantDebug:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.envSet {
				t.Setenv(debugEnvVar, test.envValue)
			}

			settings := config.Settings{Debug: test.configValue}

			applyDebugSettings(&settings, test.debugFlag)

			if settings.IsDebug() != test.wantDebug {
				t.Errorf("IsDebug() = %v, want %v (debug = %q)", settings.IsDebug(), test.wantDebug, settings.Debug)
			}
		})
	}
}

func TestParseDownloadArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantPaths []string
		wantDebug bool
	}{
		{
			name:      "config only",
			args:      []string{"config.yaml"},
			wantPaths: []string{"config.yaml"},
		},
		{
			name:      "short flag before config",
			args:      []string{"-debug", "config.yaml"},
			wantPaths: []string{"config.yaml"},
			wantDebug: true,
		},
		{
			name:      "long flag after configs",
			args:      []string{"base.yaml", "override.yaml", "--debug"},
			wantPaths: []string{"base.yaml", "override.yaml"},
			wantDebug: true,
		},
		{
			name:      "no configs",
			args:      []string{"-debug"},
			wantPaths: []string{},
			wantDebug: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, debug := parseDownloadArgs(test.args)

			if debug != test.wantDebug {
				t.Errorf("debug = %v, want %v", debug, test.wantDebug)
			}

			if len(paths) != len(test.wantPaths) {
				t.Fatalf("paths = %v, want %v", paths, test.wantPaths)
			}

			for i, path := range paths {
				if path != test.wantPaths[i] {
					t.Errorf("paths[%d] = %q, want %q", i, path, test.wantPaths[i])
				}
			}
		})
	}
}
