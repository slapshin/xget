package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"xget/src/config"
	"xget/src/storage"

	"github.com/vbauerster/mpb/v8"
)

// DownloadResult represents the result of a single file download.
type DownloadResult struct {
	File  config.FileEntry
	Error error
}

// Downloader manages parallel file downloads.
type Downloader struct {
	cfg   *config.Config
	cache *Cache
}

// NewDownloader creates a new Downloader.
func NewDownloader(cfg *config.Config, cache *Cache) *Downloader {
	return &Downloader{
		cfg:   cfg,
		cache: cache,
	}
}

// Download downloads all files from the config.
func (downloader *Downloader) Download(ctx context.Context) []DownloadResult {
	results := make([]DownloadResult, len(downloader.cfg.Files))
	resultCh := make(chan struct {
		index  int
		result DownloadResult
	}, len(downloader.cfg.Files))

	progress := mpb.NewWithContext(ctx)

	// Create worker pool.
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, downloader.cfg.Settings.Parallel)

	for i, file := range downloader.cfg.Files {
		wg.Add(1)

		go func(index int, file config.FileEntry) {
			defer wg.Done()

			semaphore <- struct{}{}

			defer func() { <-semaphore }()

			err := downloader.downloadFile(ctx, file, progress)

			resultCh <- struct {
				index  int
				result DownloadResult
			}{
				index:  index,
				result: DownloadResult{File: file, Error: err},
			}
		}(i, file)
	}

	// Wait for all downloads to complete.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results.
	for r := range resultCh {
		results[r.index] = r.result
	}

	progress.Wait()

	return results
}

func (downloader *Downloader) downloadFile(ctx context.Context, file config.FileEntry, progress *mpb.Progress) error {
	// Check if destination file already exists with correct hash.
	exists, err := downloader.checkExistingFile(file)
	if err != nil {
		return fmt.Errorf("checking existing file: %w", err)
	}

	if exists {
		fmt.Printf("skipping %s (already exists with correct hash)\n", file.Dest)

		return nil
	}

	// Try to get from cache first.
	cached := downloader.tryGetFromCache(ctx, file)
	if cached {
		return nil
	}

	// Download from source with retry.
	return downloader.downloadWithRetry(ctx, file, progress)
}

func (downloader *Downloader) tryGetFromCache(ctx context.Context, file config.FileEntry) bool {
	if downloader.cache == nil {
		return false
	}

	cached, err := downloader.cache.Get(ctx, file.SHA256, file.Dest)
	if err != nil {
		fmt.Printf("cache check error for %s: %v\n", file.Dest, err)

		return false
	}

	if cached {
		fmt.Printf("downloaded %s from cache\n", file.Dest)

		return true
	}

	return false
}

func (downloader *Downloader) downloadWithRetry(
	ctx context.Context,
	file config.FileEntry,
	progress *mpb.Progress,
) error {
	var lastErr error

	for attempt := 1; attempt <= downloader.cfg.Settings.Retries; attempt++ {
		if attempt > 1 {
			fmt.Printf("retry %d/%d for %s\n", attempt, downloader.cfg.Settings.Retries, file.URL)
			time.Sleep(downloader.cfg.Settings.RetryDelay)
		}

		err := downloader.downloadFromSource(ctx, file, progress)
		if err == nil {
			downloader.uploadToCache(ctx, file)

			return nil
		}

		lastErr = err

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return fmt.Errorf("all %d attempts: %w", downloader.cfg.Settings.Retries, lastErr)
}

func (downloader *Downloader) uploadToCache(ctx context.Context, file config.FileEntry) {
	if downloader.cache == nil {
		return
	}

	if err := downloader.cache.Put(ctx, file.SHA256, file.Dest); err != nil {
		fmt.Printf("warning: could not cache %s: %v\n", file.Dest, err)
	}
}

func (downloader *Downloader) checkExistingFile(file config.FileEntry) (bool, error) {
	info, err := os.Stat(file.Dest)
	if os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	// File exists, verify hash.
	if info.IsDir() {
		return false, fmt.Errorf("destination is a directory")
	}

	valid, err := VerifyFileSHA256(file.Dest, file.SHA256)
	if err != nil {
		return false, err
	}

	return valid, nil
}

func (downloader *Downloader) downloadFromSource(
	ctx context.Context,
	file config.FileEntry,
	progress *mpb.Progress,
) error {
	source, err := storage.NewSource(file.URL, downloader.cfg.Aliases, downloader.cfg.Settings.Timeout)
	if err != nil {
		return fmt.Errorf("creating source: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(file.Dest), 0o755)
	if err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	partialPath := file.Dest + ".partial"

	destFile, offset, err := openPartialFile(partialPath)
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}

	defer destFile.Close()

	err = downloader.performDownload(ctx, source, destFile, file, offset, progress)
	if err != nil {
		return err
	}

	return finalizeDownload(partialPath, file)
}

func openPartialFile(path string) (*os.File, int64, error) {
	info, statErr := os.Stat(path)

	if statErr == nil && info.Size() > 0 {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			return f, info.Size(), nil
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, 0, err
	}

	return f, 0, nil
}

func (downloader *Downloader) performDownload(
	ctx context.Context,
	source storage.Source,
	destFile *os.File,
	file config.FileEntry,
	offset int64,
	progressContainer *mpb.Progress,
) error {
	reader, totalSize, err := source.Download(ctx, offset)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	defer reader.Close()

	progressWriter := NewProgressWriter(progressContainer, totalSize, file.Dest)

	if offset > 0 {
		progressWriter.SetCurrent(offset)
	}

	_, err = io.Copy(io.MultiWriter(destFile, progressWriter), reader)
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	progressWriter.Finish()

	if err := destFile.Close(); err != nil {
		return fmt.Errorf("closing file: %w", err)
	}

	return nil
}

func finalizeDownload(partialPath string, file config.FileEntry) error {
	valid, err := VerifyFileSHA256(partialPath, file.SHA256)
	if err != nil {
		return fmt.Errorf("verifying checksum: %w", err)
	}

	if !valid {
		os.Remove(partialPath)

		return fmt.Errorf("checksum mismatch for %s", file.Dest)
	}

	err = os.Rename(partialPath, file.Dest)
	if err != nil {
		return fmt.Errorf("renaming file: %w", err)
	}

	return nil
}
