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
	"xget/src/output"
	"xget/src/redact"
	"xget/src/segment"
	"xget/src/storage"
)

// DownloadResult represents the result of a single file download.
type DownloadResult struct {
	File  config.FileEntry
	Error error
}

// cacheLabelPrefix marks progress and debug output for transfers served by the
// cache rather than by the origin URL.
const cacheLabelPrefix = "[cache] "

// transferJob describes a single source-to-destination transfer. The source is
// built lazily so every retry attempt starts from a fresh one.
type transferJob struct {
	newSource func(ctx context.Context) (storage.Source, error)
	dest      string
	sha256    string
	// label prefixes progress bars and debug lines for this transfer.
	label string
	// origin is a redacted, human-readable description of where bytes come from.
	origin string
}

// partialPath returns the path bytes are written to until the checksum matches.
func (job transferJob) partialPath() string {
	return job.dest + ".partial"
}

// Downloader manages parallel file downloads.
type Downloader struct {
	cfg      *config.Config
	cache    *Cache
	reporter output.Reporter
}

// NewDownloader creates a new Downloader.
func NewDownloader(cfg *config.Config, cache *Cache, reporter output.Reporter) *Downloader {
	return &Downloader{
		cfg:      cfg,
		cache:    cache,
		reporter: reporter,
	}
}

// Download downloads all files from the config.
func (downloader *Downloader) Download(ctx context.Context) []DownloadResult {
	results := make([]DownloadResult, len(downloader.cfg.Files))
	resultCh := make(chan struct {
		index  int
		result DownloadResult
	}, len(downloader.cfg.Files))

	// Create worker pool.
	var wg sync.WaitGroup

	semaphore := make(chan struct{}, downloader.cfg.Settings.Parallel)

	for i, file := range downloader.cfg.Files {
		wg.Add(1)

		go func(index int, file config.FileEntry) {
			defer wg.Done()

			semaphore <- struct{}{}

			defer func() { <-semaphore }()

			err := downloader.downloadFile(ctx, file)

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

	downloader.reporter.Wait()

	return results
}

func (downloader *Downloader) downloadFile(ctx context.Context, file config.FileEntry) error {
	downloader.reporter.Debugf("queued %s from %s (sha256 %s)", file.Dest, redact.URL(file.URL), file.SHA256)

	// Check if destination file already exists with correct hash.
	exists, err := downloader.checkExistingFile(file)
	if err != nil {
		return fmt.Errorf("checking existing file: %w", err)
	}

	if exists {
		downloader.reporter.Logf("skipping %s (already exists with correct hash)", file.Dest)

		return nil
	}

	// Try to get from cache first.
	cached := downloader.tryGetFromCache(ctx, file)
	if cached {
		downloader.reporter.Debugf("source for %s: cache", file.Dest)

		return nil
	}

	// Download from source with retry.
	err = downloader.downloadFromSource(ctx, file)
	if err != nil {
		return err
	}

	downloader.uploadToCache(ctx, file)

	return nil
}

func (downloader *Downloader) downloadFromSource(ctx context.Context, file config.FileEntry) error {
	job := transferJob{
		dest:   file.Dest,
		sha256: file.SHA256,
		label:  file.Dest,
		origin: redact.URL(file.URL),
		newSource: func(_ context.Context) (storage.Source, error) {
			return storage.NewSource(file.URL, downloader.cfg.Aliases, downloader.cfg.Settings.Timeout)
		},
	}

	return downloader.transferWithRetry(ctx, job)
}

// tryGetFromCache downloads the file from the cache when the content hash is
// present there. It uses the same segmented, resumable and retrying transfer
// path as an origin download.
func (downloader *Downloader) tryGetFromCache(ctx context.Context, file config.FileEntry) bool {
	if downloader.cache == nil {
		return false
	}

	cached, err := downloader.cache.Has(ctx, file.SHA256)
	if err != nil {
		downloader.reporter.Errorf("cache check for %s: %v", file.Dest, err)

		return false
	}

	if !cached {
		downloader.reporter.Debugf("cache miss for %s (sha256 %s)", file.Dest, file.SHA256)

		return false
	}

	downloader.reporter.Debugf("cache hit for %s (sha256 %s)", file.Dest, file.SHA256)

	job := transferJob{
		dest:   file.Dest,
		sha256: file.SHA256,
		label:  cacheLabelPrefix + file.Dest,
		origin: "cache",
		newSource: func(sourceCtx context.Context) (storage.Source, error) {
			return downloader.cache.NewSource(sourceCtx, file.SHA256)
		},
	}

	// A failed cache transfer falls back to the origin. Any .partial and
	// .segments files it leaves behind stay valid for that fallback: the cache
	// key is the content hash, so both sources deliver identical bytes.
	err = downloader.transferWithRetry(ctx, job)
	if err != nil {
		downloader.reporter.Errorf("cache download for %s: %v", file.Dest, err)

		return false
	}

	return true
}

func (downloader *Downloader) uploadToCache(ctx context.Context, file config.FileEntry) {
	if downloader.cache == nil {
		return
	}

	downloader.reporter.Debugf("uploading %s to cache (sha256 %s)", file.Dest, file.SHA256)

	err := downloader.cache.Put(ctx, file.SHA256, file.Dest)
	if err != nil {
		downloader.reporter.Errorf("could not cache %s: %v", file.Dest, err)
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

// transferWithRetry runs the transfer, retrying the configured number of times.
// The source is rebuilt on every attempt so that state cached by a source (such
// as a failed range-support probe) cannot poison later attempts.
func (downloader *Downloader) transferWithRetry(ctx context.Context, job transferJob) error {
	var lastErr error

	for attempt := 1; attempt <= downloader.cfg.Settings.Retries; attempt++ {
		err := downloader.transfer(ctx, job)
		if err == nil {
			return nil
		}

		lastErr = err

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt < downloader.cfg.Settings.Retries {
			downloader.reporter.Errorf("attempt %d/%d for %s from %s: %v, retrying in %s...",
				attempt, downloader.cfg.Settings.Retries, job.dest, job.origin,
				err, downloader.cfg.Settings.RetryDelay)
			time.Sleep(downloader.cfg.Settings.RetryDelay)
		}
	}

	return fmt.Errorf("all %d attempts: %w", downloader.cfg.Settings.Retries, lastErr)
}

func (downloader *Downloader) transfer(ctx context.Context, job transferJob) error {
	source, err := job.newSource(ctx)
	if err != nil {
		return fmt.Errorf("creating source: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(job.dest), 0o755)
	if err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	partialPath := job.partialPath()

	// Try segmented download first.
	segmented, err := downloader.trySegmentedDownload(ctx, source, job, partialPath)
	if err != nil {
		return err
	}

	if !segmented {
		err = downloader.singleStreamDownload(ctx, source, job, partialPath)
		if err != nil {
			return err
		}
	}

	err = finalizeDownload(partialPath, job)
	if err != nil {
		return err
	}

	// Clean up segment state file after successful finalization.
	if segmented {
		os.Remove(segment.StatePath(partialPath))
	}

	return nil
}

func (downloader *Downloader) trySegmentedDownload(
	ctx context.Context,
	source storage.Source,
	job transferJob,
	partialPath string,
) (bool, error) {
	if downloader.cfg.Settings.IsSingleStream() {
		return false, nil
	}

	segmentsPerFile := downloader.cfg.Settings.SegmentsPerFile
	if segmentsPerFile <= 1 {
		return false, nil
	}

	rangeSource, ok := source.(storage.RangeSource)
	if !ok {
		return false, nil
	}

	totalSize, err := rangeSource.GetSize(ctx)
	if err != nil || totalSize <= 0 {
		return false, nil //nolint:nilerr // fall back to single stream on size probe errors.
	}

	if totalSize < downloader.cfg.Settings.SegmentMinSize {
		return false, nil
	}

	acceptsRanges, err := rangeSource.AcceptsRanges(ctx)
	if err != nil || !acceptsRanges {
		return false, nil //nolint:nilerr // fall back to single stream when ranges unsupported.
	}

	downloader.reporter.Debugf("source for %s: %s (segmented, %d segments, %d bytes)",
		job.dest, job.origin, segmentsPerFile, totalSize)

	segDownloader := segment.NewDownloader(
		rangeSource,
		totalSize,
		partialPath,
		segmentsPerFile,
		downloader.reporter,
		job.label,
	)

	err = segDownloader.Download(ctx)
	if err != nil {
		return true, fmt.Errorf("segmented download: %w", err)
	}

	return true, nil
}

func (downloader *Downloader) singleStreamDownload(
	ctx context.Context,
	source storage.Source,
	job transferJob,
	partialPath string,
) error {
	// If a segment state file exists, the partial file was pre-allocated by a
	// segmented download and its size does not reflect sequential progress.
	// Remove both to start a clean single-stream download.
	statePath := segment.StatePath(partialPath)

	if _, statErr := os.Stat(statePath); statErr == nil {
		os.Remove(partialPath)
		os.Remove(statePath)
	}

	destFile, offset, err := openPartialFile(partialPath)
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}

	defer destFile.Close()

	downloader.reporter.Debugf("source for %s: %s (single stream, offset %d)",
		job.dest, job.origin, offset)

	return downloader.performDownload(ctx, source, destFile, job, offset)
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
	job transferJob,
	offset int64,
) error {
	reader, totalSize, err := source.Download(ctx, offset)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	defer reader.Close()

	tracker := downloader.reporter.NewTracker(totalSize, job.label)
	defer tracker.Abort()

	if offset > 0 {
		tracker.SetCurrent(offset)
	}

	_, err = io.Copy(io.MultiWriter(destFile, tracker), reader)
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	tracker.Finish()

	if err := destFile.Close(); err != nil {
		return fmt.Errorf("closing file: %w", err)
	}

	return nil
}

func finalizeDownload(partialPath string, job transferJob) error {
	valid, err := VerifyFileSHA256(partialPath, job.sha256)
	if err != nil {
		return fmt.Errorf("verifying checksum: %w", err)
	}

	if !valid {
		os.Remove(partialPath)
		os.Remove(segment.StatePath(partialPath))

		return fmt.Errorf("checksum mismatch for %s", job.dest)
	}

	err = os.Rename(partialPath, job.dest)
	if err != nil {
		return fmt.Errorf("renaming file: %w", err)
	}

	return nil
}
