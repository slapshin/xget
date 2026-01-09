package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"xget/src/config"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config.yaml>\n", os.Args[0])

		return 1
	}

	configPath := os.Args[1]

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)

		return 1
	}

	fmt.Printf("Loaded config with %d files to download\n", len(cfg.Files))

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
