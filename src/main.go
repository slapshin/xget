package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"xget/src/config"
	"xget/src/output"
	"xget/src/redact"
)

// debugEnvVar enables debug output without editing the config file.
const debugEnvVar = "XGET_DEBUG"

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	fmt.Printf("xget %s (commit: %s, built: %s)\n", version, commit, date)

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-debug] <config.yaml> [<config2.yaml> ...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s generate <directory> [-o output.yaml]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s -version\n", os.Args[0])

		return 1
	}

	if os.Args[1] == "generate" {
		return runGenerate()
	}

	if os.Args[1] == "-version" || os.Args[1] == "--version" {
		fmt.Printf("xget version %s (commit: %s, built: %s)\n", version, commit, date)

		return 0
	}

	return runDownload(os.Args[1:])
}

// runDownload loads the configs given in args and downloads every file in them.
func runDownload(args []string) int {
	configPaths, debugFlag := parseDownloadArgs(args)

	if len(configPaths) == 0 {
		fmt.Fprintf(os.Stderr, "error: no config files specified\n")

		return 1
	}

	cfg, err := config.LoadMultiple(configPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)

		return 1
	}

	if len(configPaths) > 1 {
		fmt.Printf("Loaded %d config files with %d files to download\n", len(configPaths), len(cfg.Files))
	} else {
		fmt.Printf("Loaded config with %d files to download\n", len(cfg.Files))
	}

	applyDebugSettings(&cfg.Settings, debugFlag)

	printConfig(*cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cancelling downloads...")
		cancel()
	}()

	reporter := newReporter(ctx, cfg.Settings)

	cache := NewCache(cfg)
	if cache != nil {
		reporter.Logf("cache enabled")
	}

	downloader := NewDownloader(cfg, cache, reporter)
	results := downloader.Download(ctx)

	failed := reportResults(results)
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d/%d downloads failed\n", failed, len(results))

		return 1
	}

	fmt.Printf("\nAll %d downloads completed successfully\n", len(results))

	return 0
}

// applyDebugSettings resolves debug mode from its three sources, in increasing
// order of precedence: settings.debug in the config, the XGET_DEBUG env var
// (which also disables debug when set to a falsy value) and the -debug flag.
func applyDebugSettings(settings *config.Settings, debugFlag bool) {
	value, isSet := os.LookupEnv(debugEnvVar)
	if isSet {
		settings.Debug = strings.TrimSpace(value)
	}

	if debugFlag {
		settings.Debug = "true"
	}
}

// newReporter creates the reporter matching the configured output mode.
// Debug mode replaces the progress bars with raw lines and periodic stats.
func newReporter(ctx context.Context, settings config.Settings) output.Reporter {
	if !settings.IsDebug() {
		return output.NewBarReporter(ctx, os.Stdout)
	}

	reporter := output.NewDebugReporter(ctx, os.Stdout, os.Stderr, settings.DebugInterval)
	reporter.Logf("debug mode enabled, stats every %s", settings.DebugInterval)

	return reporter
}

// parseDownloadArgs splits the download arguments into config paths and the
// debug flag.
func parseDownloadArgs(args []string) ([]string, bool) {
	paths := make([]string, 0, len(args))
	debug := false

	for _, arg := range args {
		if arg == "-debug" || arg == "--debug" {
			debug = true

			continue
		}

		paths = append(paths, arg)
	}

	return paths, debug
}

func reportResults(results []DownloadResult) int {
	var failed int

	for _, result := range results {
		if result.Error != nil {
			fmt.Fprintf(os.Stderr, "error downloading %s: %v\n", redact.URL(result.File.URL), result.Error)

			failed++
		}
	}

	return failed
}

func runGenerate() int {
	args := os.Args[2:]

	var outputFile string

	i := 0
	for i < len(args) {
		if args[i] == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: -o flag requires an argument\n")

				return 1
			}

			outputFile = args[i+1]
			args = append(args[:i], args[i+2:]...)
		} else {
			i++
		}
	}

	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "error: generate command requires exactly one directory argument\n")
		fmt.Fprintf(os.Stderr, "Usage: %s generate <directory> [-o output.yaml]\n", os.Args[0])

		return 1
	}

	dirPath := args[0]

	data, err := generateConfig(dirPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating config: %v\n", err)

		return 1
	}

	if outputFile != "" {
		err = os.WriteFile(outputFile, data, 0o600) //nolint:gosec // path is from CLI argument
		if err != nil {
			fmt.Fprintf(os.Stderr, "error writing output file: %v\n", err)

			return 1
		}

		fmt.Printf("generated config written to %s\n", outputFile)
	} else {
		fmt.Print(string(data))
	}

	return 0
}
