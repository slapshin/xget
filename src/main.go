package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"xget/src/config"
)

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
		fmt.Fprintf(os.Stderr, "Usage: %s <config.yaml> [<config2.yaml> ...]\n", os.Args[0])
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

	configPaths := os.Args[1:]

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cancelling downloads...")
		cancel()
	}()

	cache := NewCache(cfg)
	if cache != nil {
		fmt.Println("Cache enabled")
	}

	downloader := NewDownloader(cfg, cache)
	results := downloader.Download(ctx)

	failed := reportResults(results)
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d/%d downloads failed\n", failed, len(results))

		return 1
	}

	fmt.Printf("\nAll %d downloads completed successfully\n", len(results))

	return 0
}

func reportResults(results []DownloadResult) int {
	var failed int

	for _, result := range results {
		if result.Error != nil {
			fmt.Fprintf(os.Stderr, "error downloading %s: %v\n", result.File.URL, result.Error)

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
		err = os.WriteFile(outputFile, data, 0o600)
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
